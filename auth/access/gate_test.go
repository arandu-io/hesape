package access_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/access"
)

type post struct {
	ID       string
	AuthorID string
}

// Owner makes a post ownable, so a policy registered for the interface covers it.
func (p post) Owner() string { return p.AuthorID }

type ownable interface{ Owner() string }

// postPolicy decides for a post: one method per ability, named after it.
type postPolicy struct {
	access.HandlesAuthorization
}

func (postPolicy) Update(_ context.Context, user auth.Subject, arguments ...any) any {
	p, ok := arguments[0].(post)

	return ok && p.AuthorID == user.ID
}

func (p postPolicy) Delete(_ context.Context, _ auth.Subject, _ ...any) any {
	return p.DenyAsNotFound("no post with that id", "post.missing")
}

// ViewAny answers the "view-any" ability, and the "viewAny" one Resource
// defines: a dashed ability names a camel-cased method, uppercased because a Go
// method another package calls is an exported one.
func (postPolicy) ViewAny(_ context.Context, user auth.Subject, arguments ...any) any {
	return len(arguments) == 0 && user.HasRole("author")
}

// auditedPostPolicy is postPolicy with a before method, which is how a policy
// grants an administrator every ability it defines without repeating the test.
type auditedPostPolicy struct {
	postPolicy
}

func (auditedPostPolicy) Before(_ context.Context, user auth.Subject, _ string, _ ...any) any {
	if user.HasRole("admin") {
		return true
	}

	return nil
}

type ownablePolicy struct{}

func (ownablePolicy) Update(_ context.Context, _ auth.Subject, _ ...any) any { return true }

var (
	ada    = auth.Subject{ID: "u1", Tenant: "acme", Roles: []string{"author"}}
	bruno  = auth.Subject{ID: "u2", Tenant: "acme"}
	admin  = auth.Subject{ID: "u3", Tenant: "acme", Roles: []string{"admin"}}
	hers   = post{ID: "p1", AuthorID: "u1"}
	always = func(answer any) access.Ability {
		return func(_ context.Context, _ auth.Subject, _ ...any) any { return answer }
	}
)

func TestHasReportsOnlyTheAbilitiesThatWereDefined(t *testing.T) {
	gate := access.NewGate().Define("posts.update", always(true)).Define("posts.delete", always(false))

	if !gate.Has("posts.update", "posts.delete") {
		t.Fatal("Has said no about two defined abilities")
	}
	if gate.Has("posts.update", "posts.publish") {
		t.Fatal("Has said yes with one ability undefined -- it answers about all of them")
	}
	if !gate.Has() {
		t.Fatal("Has said no about nothing at all")
	}
}

func TestAllowsAndDeniesRunTheDefinedAbility(t *testing.T) {
	gate := access.NewGate().Define("posts.update", func(_ context.Context, user auth.Subject, arguments ...any) any {
		return arguments[0].(post).AuthorID == user.ID
	})

	if !gate.Allows(context.Background(), ada, "posts.update", hers) {
		t.Fatal("the author was denied her own post")
	}
	if gate.Denies(context.Background(), ada, "posts.update", hers) {
		t.Fatal("Denies disagreed with Allows")
	}
	if gate.Allows(context.Background(), bruno, "posts.update", hers) {
		t.Fatal("somebody else was allowed to update the post")
	}
}

func TestAnUndefinedAbilityIsDenied(t *testing.T) {
	if access.NewGate().Allows(context.Background(), ada, "posts.publish") {
		t.Fatal("an ability nobody defined was granted")
	}
}

func TestCheckRequiresEveryAbility(t *testing.T) {
	gate := access.NewGate().Define("posts.view", always(true)).Define("posts.update", always(false))

	if !gate.Check(context.Background(), ada, []string{"posts.view"}) {
		t.Fatal("Check denied a granted ability")
	}
	if gate.Check(context.Background(), ada, []string{"posts.view", "posts.update"}) {
		t.Fatal("Check granted a list with a denied ability in it")
	}
	if !gate.Check(context.Background(), ada, nil) {
		t.Fatal("Check denied an empty list -- Collection::every answers true for one")
	}
}

func TestAnyRequiresOneAbilityAndNoneIsItsOpposite(t *testing.T) {
	gate := access.NewGate().Define("posts.view", always(true)).Define("posts.update", always(false))

	abilities := []string{"posts.update", "posts.view"}
	if !gate.Any(context.Background(), ada, abilities) {
		t.Fatal("Any denied a list with a granted ability in it")
	}
	if gate.None(context.Background(), ada, abilities) {
		t.Fatal("None disagreed with Any")
	}

	denied := []string{"posts.update"}
	if gate.Any(context.Background(), ada, denied) {
		t.Fatal("Any granted a list with nothing granted in it")
	}
	if !gate.None(context.Background(), ada, denied) {
		t.Fatal("None disagreed with Any")
	}

	if gate.Any(context.Background(), ada, nil) {
		t.Fatal("Any granted an empty list -- Collection::contains answers false for one")
	}
}

func TestBeforeCallbackShortCircuitsTheCheck(t *testing.T) {
	ran := false

	gate := access.NewGate().Define("posts.update", func(_ context.Context, _ auth.Subject, _ ...any) any {
		ran = true

		return false
	})
	gate.Before(func(_ context.Context, user auth.Subject, _ string, _ []any) any {
		if user.HasRole("admin") {
			return true
		}

		return nil
	})

	if !gate.Allows(context.Background(), admin, "posts.update", hers) {
		t.Fatal("the before callback did not grant the administrator")
	}
	if ran {
		t.Fatal("the ability ran after the before callback had answered")
	}

	if gate.Allows(context.Background(), ada, "posts.update", hers) {
		t.Fatal("the ability was not consulted after the before callback answered nil")
	}
	if !ran {
		t.Fatal("a nil answer from the before callback did not let the check carry on")
	}
}

func TestBeforeCallbackCanDenyOutright(t *testing.T) {
	gate := access.NewGate().Define("posts.update", always(true))
	gate.Before(func(_ context.Context, _ auth.Subject, _ string, _ []any) any {
		return access.DenyAsNotFound("the account is suspended", nil)
	})

	response := gate.Inspect(context.Background(), ada, "posts.update", hers)
	if response.Allowed() {
		t.Fatal("the before callback's denial was ignored")
	}
	if response.Status() == nil || *response.Status() != http.StatusNotFound {
		t.Fatalf("status = %v, want the 404 the before callback asked for", response.Status())
	}
}

func TestAfterCallbackCannotOverturnADecision(t *testing.T) {
	var seen any

	gate := access.NewGate().Define("posts.update", always(false))
	gate.After(func(_ context.Context, _ auth.Subject, _ string, result any, _ []any) any {
		seen = result

		return true
	})

	if gate.Allows(context.Background(), ada, "posts.update", hers) {
		t.Fatal("the after callback overturned a denial -- it may only fill in a result nobody gave")
	}
	if seen != false {
		t.Fatalf("the after callback saw %v, want the false the ability answered", seen)
	}
}

func TestAfterCallbackFillsInAResultNobodyGave(t *testing.T) {
	gate := access.NewGate()
	gate.After(func(_ context.Context, _ auth.Subject, _ string, _ any, _ []any) any { return true })
	gate.After(func(_ context.Context, _ auth.Subject, _ string, _ any, _ []any) any { return false })

	if !gate.Allows(context.Background(), ada, "posts.update") {
		t.Fatal("the first after callback did not fill in the nil result, or the second replaced it")
	}
}

func TestInspectAnswersTheDefaultDenialResponse(t *testing.T) {
	gate := access.NewGate().Define("posts.update", always(false))

	if message := gate.Inspect(context.Background(), ada, "posts.update", hers).Message(); message != "" {
		t.Fatalf("message = %q, want the empty default denial", message)
	}

	gate.DefaultDenialResponse(access.DenyAsNotFound("nothing here", "gone"))

	response := gate.Inspect(context.Background(), ada, "posts.update", hers)
	if response.Allowed() || response.Message() != "nothing here" {
		t.Fatalf("Inspect = %v, want the default denial", response.ToArray())
	}
}

func TestInspectKeepsTheResponseAnAbilityAnswered(t *testing.T) {
	gate := access.NewGate().Define("posts.delete", always(access.DenyAsNotFound("no post with that id", "post.missing")))

	response := gate.Inspect(context.Background(), ada, "posts.delete", hers)
	if response.Allowed() {
		t.Fatal("the denial was read as an allow")
	}
	if response.Code() != "post.missing" || response.Status() == nil || *response.Status() != http.StatusNotFound {
		t.Fatalf("Inspect = %v, status %v", response.ToArray(), response.Status())
	}
}

func TestInspectTurnsAFailureIntoADenial(t *testing.T) {
	gate := access.NewGate().
		Define("posts.delete", always(access.NewAuthorizationError("no post with that id", "post.missing", nil).AsNotFound())).
		Define("posts.update", always(errors.New("the row was gone")))

	response := gate.Inspect(context.Background(), ada, "posts.delete", hers)
	if response.Allowed() || response.Status() == nil || *response.Status() != http.StatusNotFound {
		t.Fatalf("an AuthorizationError became %v, status %v", response.ToArray(), response.Status())
	}

	if response := gate.Inspect(context.Background(), ada, "posts.update", hers); response.Allowed() {
		t.Fatal("an ability that answered with a failure was read as an allow")
	}
}

func TestRawAnswersWhatTheAbilityAnswered(t *testing.T) {
	allow := access.Allow("because", nil)
	gate := access.NewGate().Define("posts.update", always(allow))

	if raw := gate.Raw(context.Background(), ada, "posts.update", hers); raw != any(allow) {
		t.Fatalf("Raw = %v, want the very response the ability answered", raw)
	}
	if raw := gate.Raw(context.Background(), ada, "posts.publish"); raw != nil {
		t.Fatalf("Raw on an undefined ability = %v, want nil", raw)
	}
}

func TestAuthorizeIssuesAGrantThatPassesCheck(t *testing.T) {
	gate := access.NewGate().Define("posts.update", func(_ context.Context, user auth.Subject, arguments ...any) any {
		return arguments[0].(post).AuthorID == user.ID
	})

	grant, err := gate.Authorize(context.Background(), ada, "posts.update", hers)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := grant.Check("posts.update"); err != nil {
		t.Fatalf("the Gate's Grant did not pass Check on its own action: %v", err)
	}
	if err := grant.Check("posts.delete"); err == nil {
		t.Fatal("the Grant passed Check on an action nobody authorized")
	}
	if grant.Subject().ID != ada.ID || auth.Tenant(grant) != "acme" {
		t.Fatalf("grant subject = %v, tenant = %q", grant.Subject(), auth.Tenant(grant))
	}
}

func TestAuthorizeFailsWithTheDenialTheAbilityProduced(t *testing.T) {
	gate := access.NewGate().Define("posts.delete", always(access.DenyAsNotFound("no post with that id", "post.missing")))

	grant, err := gate.Authorize(context.Background(), ada, "posts.delete", hers)
	if err == nil {
		t.Fatal("Authorize granted a denied ability")
	}
	if err := grant.Check("posts.delete"); err == nil {
		t.Fatal("the Grant returned alongside the error passed Check")
	}
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("error = %v, want auth.ErrForbidden -- the exception handler answers 403 on that alone", err)
	}

	var authorization *access.AuthorizationError
	if !errors.As(err, &authorization) {
		t.Fatalf("error = %T, want *access.AuthorizationError carrying the response", err)
	}
	if !authorization.HasStatus() || *authorization.Status() != http.StatusNotFound {
		t.Fatalf("status = %v, want the 404 the ability asked for", authorization.Status())
	}
	if response := authorization.Response(); response == nil || response.Code() != "post.missing" {
		t.Fatalf("the error does not carry the response the ability produced: %v", response)
	}

	cause := errors.Unwrap(authorization)
	if cause == nil || !strings.Contains(cause.Error(), "posts.delete") || !strings.Contains(cause.Error(), ada.ID) {
		t.Fatalf("cause = %v, want the sentence naming the action and the subject", cause)
	}
}

func TestAuthorizeRefusesAnAnonymousSubjectBeforeTheAbilityRuns(t *testing.T) {
	ran := false

	gate := access.NewGate().Define("posts.update", func(_ context.Context, _ auth.Subject, _ ...any) any {
		ran = true

		return true
	})

	_, err := gate.Authorize(context.Background(), auth.Subject{}, "posts.update", hers)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("error = %v, want auth.ErrForbidden", err)
	}
	if ran {
		t.Fatal("the ability was consulted about a subject nobody loaded")
	}
}

func TestAuthorizeReachesADeclaredGuest(t *testing.T) {
	gate := access.NewGate().Define("posts.view", func(_ context.Context, user auth.Subject, _ ...any) any {
		return user.IsGuest()
	})

	grant, err := gate.Authorize(context.Background(), auth.Guest("acme"), "posts.view", hers)
	if err != nil {
		t.Fatalf("Authorize for a declared guest: %v", err)
	}
	if err := grant.Check("posts.view"); err != nil {
		t.Fatalf("the guest's Grant did not pass Check: %v", err)
	}
}

func TestPolicyMethodDecidesForItsModel(t *testing.T) {
	gate := access.NewGate().Policy(post{}, postPolicy{})

	if !gate.Allows(context.Background(), ada, "update", hers) {
		t.Fatal("the policy denied the author her own post")
	}
	if gate.Allows(context.Background(), bruno, "update", hers) {
		t.Fatal("the policy allowed somebody else")
	}

	response := gate.Inspect(context.Background(), ada, "delete", hers)
	if response.Allowed() || response.Code() != "post.missing" {
		t.Fatalf("delete = %v, want the policy's own denial", response.ToArray())
	}
}

func TestADashedAbilityNamesTheCamelCasedMethod(t *testing.T) {
	gate := access.NewGate().Policy(post{}, postPolicy{})

	// A reflect.Type stands for the collection case, and it is dropped before
	// the policy is called: the policy already knows what it decides for.
	if !gate.Allows(context.Background(), ada, "view-any", reflect.TypeOf(post{})) {
		t.Fatal("view-any did not reach ViewAny, or the type was passed on to it")
	}
	if gate.Allows(context.Background(), bruno, "view-any", reflect.TypeOf(post{})) {
		t.Fatal("ViewAny allowed somebody with no author role")
	}
}

func TestAPolicyBeforeMethodShortCircuitsTheCheck(t *testing.T) {
	gate := access.NewGate().Policy(post{}, auditedPostPolicy{})

	if !gate.Allows(context.Background(), admin, "update", hers) {
		t.Fatal("the policy's before method did not grant the administrator")
	}
	if gate.Allows(context.Background(), bruno, "update", hers) {
		t.Fatal("a nil answer from before did not let the policy method decide")
	}
	if !gate.Allows(context.Background(), ada, "update", hers) {
		t.Fatal("the policy method was not reached after before answered nil")
	}
}

func TestAnAbilityThePolicyDoesNotHaveFallsThroughToTheDefinedOne(t *testing.T) {
	gate := access.NewGate().Policy(post{}, postPolicy{}).Define("archive", always(true))

	if !gate.Allows(context.Background(), bruno, "archive", hers) {
		t.Fatal("the check stopped at a policy with no method for the ability")
	}
}

func TestGetPolicyForFindsThePolicyByType(t *testing.T) {
	gate := access.NewGate().Policy(post{}, postPolicy{})

	for name, class := range map[string]any{
		"a value":     hers,
		"a pointer":   &hers,
		"a type":      reflect.TypeOf(post{}),
		"a type ptr":  reflect.TypeOf(&hers),
		"the pointer": new(post),
	} {
		if gate.GetPolicyFor(class) == nil {
			t.Errorf("GetPolicyFor(%s) found nothing", name)
		}
	}

	if gate.GetPolicyFor("posts") != nil {
		t.Error("GetPolicyFor found a policy for an unrelated type")
	}
	if gate.GetPolicyFor(nil) != nil {
		t.Error("GetPolicyFor found a policy for nothing at all")
	}
}

func TestGetPolicyForFindsTheOneRegisteredForAnInterface(t *testing.T) {
	gate := access.NewGate().Policy(reflect.TypeOf((*ownable)(nil)).Elem(), ownablePolicy{})

	if gate.GetPolicyFor(hers) == nil {
		t.Fatal("a policy registered for an interface did not cover a type implementing it")
	}
	if !gate.Allows(context.Background(), bruno, "update", hers) {
		t.Fatal("the interface's policy was not consulted")
	}
}

func TestResourceDefinesTheAbilitiesOfAPolicy(t *testing.T) {
	gate := access.NewGate().Resource("posts", postPolicy{}, nil)

	if !gate.Has("posts.viewAny", "posts.view", "posts.create", "posts.update", "posts.delete") {
		t.Fatalf("Resource defined %v, want the five default abilities", sortedAbilities(gate))
	}
	if !gate.Allows(context.Background(), ada, "posts.update", hers) {
		t.Fatal("posts.update did not reach the policy's Update")
	}
	if !gate.Allows(context.Background(), ada, "posts.viewAny") {
		t.Fatal("posts.viewAny did not reach the policy's ViewAny")
	}
	if gate.Allows(context.Background(), ada, "posts.view", hers) {
		t.Fatal("posts.view was granted by a policy with no View method")
	}
}

func TestResourceTakesTheAbilitiesItIsGiven(t *testing.T) {
	gate := access.NewGate().Resource("posts", auditedPostPolicy{}, map[string]string{"edit": "update"})

	if !gate.Has("posts.edit") || gate.Has("posts.update") {
		t.Fatalf("Resource defined %v, want posts.edit alone", sortedAbilities(gate))
	}
	if !gate.Allows(context.Background(), admin, "posts.edit", hers) {
		t.Fatal("the policy's before method was not called for a Resource ability")
	}
	if !gate.Allows(context.Background(), ada, "posts.edit", hers) {
		t.Fatal("posts.edit did not reach Update")
	}
	if gate.Allows(context.Background(), bruno, "posts.edit", hers) {
		t.Fatal("posts.edit allowed somebody else")
	}
}

func TestForUserFillsInASubjectTheCallDidNotName(t *testing.T) {
	gate := access.NewGate().Define("posts.update", func(_ context.Context, user auth.Subject, _ ...any) any {
		return user.ID == ada.ID
	})

	hers := gate.ForUser(ada)

	if !hers.Allows(context.Background(), auth.Subject{}, "posts.update") {
		t.Fatal("the subject ForUser was given did not fill in for the zero one")
	}
	if hers.Allows(context.Background(), bruno, "posts.update") {
		t.Fatal("the subject the call named did not win")
	}
	if !hers.Has("posts.update") {
		t.Fatal("the clone did not carry the abilities")
	}

	hers.Define("posts.delete", always(true))
	if gate.Has("posts.delete") {
		t.Fatal("defining on the clone reached back into the gate it came from")
	}
}

func TestAllowIfAndDenyIfCheckOnDemand(t *testing.T) {
	gate := access.NewGate()

	if _, err := gate.AllowIf(context.Background(), ada, true, "", nil); err != nil {
		t.Fatalf("AllowIf(true): %v", err)
	}
	if _, err := gate.AllowIf(context.Background(), ada, false, "not yours", nil); err == nil {
		t.Fatal("AllowIf(false) did not fail")
	}
	if _, err := gate.DenyIf(context.Background(), ada, false, "", nil); err != nil {
		t.Fatalf("DenyIf(false): %v", err)
	}
	if _, err := gate.DenyIf(context.Background(), ada, true, "not yours", nil); err == nil {
		t.Fatal("DenyIf(true) did not fail")
	}

	condition := func(_ context.Context, user auth.Subject) any { return user.HasRole("admin") }
	if _, err := gate.AllowIf(context.Background(), admin, condition, "not an administrator", nil); err != nil {
		t.Fatalf("AllowIf with a closure: %v", err)
	}

	_, err := gate.AllowIf(context.Background(), ada, condition, "not an administrator", "role.missing")
	if err == nil {
		t.Fatal("AllowIf with a closure that answered false did not fail")
	}
	if err.Error() != "not an administrator" {
		t.Fatalf("message = %q, want the one AllowIf was given", err.Error())
	}

	_, err = gate.AllowIf(context.Background(), ada, access.DenyAsNotFound("no post with that id", nil), "", nil)

	var authorization *access.AuthorizationError
	if !errors.As(err, &authorization) || !authorization.HasStatus() {
		t.Fatalf("AllowIf with a Response = %v, want its status kept", err)
	}
}

func TestAbilitiesAndPoliciesAreCopies(t *testing.T) {
	gate := access.NewGate().Define("posts.update", always(true)).Policy(post{}, postPolicy{})

	abilities := gate.Abilities()
	if len(abilities) != 1 {
		t.Fatalf("Abilities = %v, want one", sortedAbilities(gate))
	}
	delete(abilities, "posts.update")

	policies := gate.Policies()
	if len(policies) != 1 {
		t.Fatalf("Policies = %v, want one", policies)
	}
	clear(policies)

	if !gate.Has("posts.update") {
		t.Fatal("deleting from what Abilities handed back changed the gate")
	}
	if gate.GetPolicyFor(hers) == nil {
		t.Fatal("clearing what Policies handed back changed the gate")
	}
}

func sortedAbilities(gate *access.Gate) []string {
	names := make([]string, 0, len(gate.Abilities()))
	for name := range gate.Abilities() {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}
