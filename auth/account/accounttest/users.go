package accounttest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/validation"
)

func userCases() []testCase {
	return []testCase{
		{"RegistrationStoresNullUntilTheAddressIsVerified", registrationStoresNull},
		{"VerificationChangesANullRegistrationExactlyOnce", verificationExactlyOnce},
		{"AVerificationForAnOlderAddressIsRefused", verificationForAnOlderAddress},
		{"ADuplicateAddressIsRecognisedByTheDriverCode", duplicateAddress},
		{"RegistrationRefusesWhatItCannotStore", registrationRefusesInput},
		{"RegistrationJoinsTheCallersTransaction", registrationJoinsTransaction},
		{"VerifyCredentialsAnswersEveryFailureAlike", verifyCredentialsFailures},
		{"ASignInRewritesAWeakerHash", signInRewritesWeakerHash},
		{"ProvisionIsIdempotentAndVerifiesOnRequest", provisionIdempotent},
		{"LookupOfAnUnknownAddressIsNotFound", lookupUnknown},
		{"AResetLinkIsSpentByTheResetItMakes", resetLinkSpent},
		{"ConfirmPasswordAnswersAWrongPasswordAsInvalidCredentials", confirmPassword},
		{"ChangePasswordNeedsTheCurrentOneAndStoresItsEvent", changePassword},
		{"RenameChangesTheNameAndStoresItsEvent", rename},
		{"CloseReleasesTheAddressAndLocksThePassword", closeAccount},
		{"PublicNamesStayInsideTheReadersTenant", publicNames},
		{"EventsCarryTheSamePayloadAsBefore", eventPayloads},
	}
}

func registrationStoresNull(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	if user.IsVerified() {
		t.Fatal("a new registration is already verified")
	}
	if user.AccountID() == "" || user.AccountTenant() != tenantA {
		t.Fatalf("the registered account is id=%q tenant=%q, want an id in %s", user.AccountID(), user.AccountTenant(), tenantA)
	}
	if v := e.value(`SELECT verified_at FROM users WHERE id = ?`, user.AccountID()); !isNull(v) {
		t.Fatalf("a new registration stored verified_at = %v, want SQL NULL", v)
	}
	if got := e.eventCount(user.AccountID(), account.UserRegistered); got != 1 {
		t.Fatalf("registration stored %d %s events, want one", got, account.UserRegistered)
	}
	encoded, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("marshalling the account: %v", err)
	}
	if strings.Contains(string(encoded), user.PasswordHash()) {
		t.Fatalf("the account's JSON carries its password hash: %s", encoded)
	}
}

func verificationExactlyOnce(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")

	const attempts = 8
	changed := make([]bool, attempts)
	verified := make([]bool, attempts)
	index := make(chan int, attempts)
	for i := range attempts {
		index <- i
	}
	errs := race(attempts, func() error {
		i := <-index
		user, won, err := users.MarkVerified(e.ctx, registered.AccountTenant(), registered.AccountID(), registered.AccountEmail())
		if err == nil {
			changed[i], verified[i] = won, user.IsVerified()
		}
		return err
	})
	winners := 0
	for i, err := range errs {
		if err != nil {
			t.Errorf("verification attempt %d: %v", i, err)
			continue
		}
		if !verified[i] {
			t.Errorf("verification attempt %d answered an unverified account", i)
		}
	}
	for _, won := range changed {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("verification produced %d state changes, want exactly one", winners)
	}
	if got := e.eventCount(registered.AccountID(), account.EmailVerified); got != 1 {
		t.Fatalf("verification stored %d email-verified events, want exactly one", got)
	}
	if v := e.value(`SELECT verified_at FROM users WHERE id = ?`, registered.AccountID()); isNull(v) {
		t.Fatal("the winning verification stored no verified_at")
	}
	found, err := users.FindForAuthentication(e.ctx, tenantA, registered.AccountID())
	if err != nil || !found.IsVerified() {
		t.Fatalf("the account read back is verified=%v (%v), want verified", err == nil && found.IsVerified(), err)
	}
}

func verificationForAnOlderAddress(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	if _, _, err := users.MarkVerified(e.ctx, tenantA, registered.AccountID(), "old@example.test"); !errors.Is(err, account.ErrVerificationAddressChanged) {
		t.Fatalf("a code for another address answered %v, want ErrVerificationAddressChanged", err)
	}
	if _, _, err := users.MarkVerified(e.ctx, tenantB, registered.AccountID(), registered.AccountEmail()); !errors.Is(err, account.ErrUserNotFound) {
		t.Fatalf("a code verified in another tenant answered %v, want ErrUserNotFound", err)
	}
}

// duplicateAddress registers one address twice in a tenant. The second write
// is refused by the unique index, and the error says so twice over: as the
// account rule the screens check for, and as the unique violation the router
// answers with 409. It is never read off the driver's message.
func duplicateAddress(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	e.register(users, tenantA, "Ana", "ana@example.test")

	_, err := users.Register(e.ctx, tenantA, account.Registration{Name: "Ana again", Email: " ANA@example.test ", Password: password})
	if !errors.Is(err, account.ErrEmailTaken) {
		t.Fatalf("the second registration answered %v, want ErrEmailTaken", err)
	}
	if !errors.Is(err, database.ErrUniqueViolation) {
		t.Fatalf("the second registration answered %v, want it to match database.ErrUniqueViolation", err)
	}
	// The same address in another tenant is another account.
	e.register(users, tenantB, "Ana", "ana@example.test")
}

func registrationRefusesInput(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	for name, registration := range map[string]account.Registration{
		"no name":            {Name: "  ", Email: "ana@example.test", Password: password},
		"no address":         {Name: "Ana", Email: " ", Password: password},
		"a short password":   {Name: "Ana", Email: "ana@example.test", Password: "short"},
		"an empty password":  {Name: "Ana", Email: "ana@example.test"},
		"nothing filled in":  {},
		"a password of 11":   {Name: "Ana", Email: "ana@example.test", Password: "elevenchars"},
		"7 accented chars":   {Name: "Ana", Email: "ana@example.test", Password: "ççççççç"},
		"a password of 129":  {Name: "Ana", Email: "ana@example.test", Password: strings.Repeat("a", 129)},
		"spaces for a name":  {Name: "\t", Email: "ana@example.test", Password: password},
		"spaces for address": {Name: "Ana", Email: "\n", Password: password},
	} {
		if _, err := users.Register(e.ctx, tenantA, registration); err == nil {
			t.Errorf("a registration with %s was stored", name)
		}
	}
	if n := e.count(`SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("refused registrations left %d rows", n)
	}
}

func registrationJoinsTransaction(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	refused := errors.New("the caller's next step failed")
	var id string
	err := database.Transaction(e.ctx, e.f.DB, func(ctx context.Context) error {
		user, err := users.Register(ctx, tenantA, account.Registration{Name: "Ana", Email: "ana@example.test", Password: password})
		if err != nil {
			return err
		}
		id = user.AccountID()
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("the transaction answered %v, want the caller's error", err)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE id = ?`, id); n != 0 {
		t.Fatal("the account survived the rollback of the transaction it was registered in")
	}
	if got := len(e.events(id)); got != 0 {
		t.Fatalf("%d events survived the rollback of the transaction they were stored in", got)
	}
}

func verifyCredentialsFailures(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")

	user, err := users.VerifyCredentials(e.ctx, tenantA, " ANA@example.test ", password, "127.0.0.1")
	if err != nil {
		t.Fatalf("the right password answered %v", err)
	}
	if user.AccountID() != registered.AccountID() {
		t.Fatalf("the right password returned account %q, want %q", user.AccountID(), registered.AccountID())
	}
	for name, attempt := range map[string][3]string{
		"a wrong password":     {tenantA, "ana@example.test", "not-the-password-at-all"},
		"an unknown address":   {tenantA, "nobody@example.test", password},
		"another tenant":       {tenantB, "ana@example.test", password},
		"an empty password":    {tenantA, "ana@example.test", ""},
		"an address only case": {tenantA, "ANA@EXAMPLE.TEST", "wrong-password-here"},
	} {
		if _, err := users.VerifyCredentials(e.ctx, attempt[0], attempt[1], attempt[2], "127.0.0.2"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("%s answered %v, want auth.ErrInvalidCredentials", name, err)
		}
	}
}

// signInRewritesWeakerHash provisions an account carrying a bcrypt hash, the
// shape an import holds, and signs in with it: the sign-in succeeds and the
// column now holds argon2id, written through the application's users query.
func signInRewritesWeakerHash(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	imported := bcryptOf(t, password)
	user, err := users.Provision(e.ctx, tenantA, account.Draft{Name: "Ana", Email: "ana@example.test", PasswordHash: imported})
	if err != nil {
		t.Fatalf("provisioning with a bcrypt hash: %v", err)
	}
	signedIn, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", password, "127.0.0.1")
	if err != nil {
		t.Fatalf("signing in with the imported hash: %v", err)
	}
	stored := e.text(`SELECT password FROM users WHERE id = ?`, user.AccountID())
	if !argon2id(stored) {
		t.Fatalf("the column after sign-in holds %q, want an argon2id hash", stored)
	}
	if signedIn.PasswordHash() != stored {
		t.Fatal("the account the sign-in returned does not hold the hash it stored")
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", password, "127.0.0.1"); err != nil {
		t.Fatalf("signing in with the rewritten hash: %v", err)
	}
}

func provisionIdempotent(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	first, err := users.Provision(e.ctx, tenantA, account.Draft{Name: "Admin", Email: "Admin@Example.test", Password: password, Verified: true})
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if !first.IsVerified() || first.AccountEmail() != "admin@example.test" {
		t.Fatalf("the provisioned account is verified=%v email=%q", first.IsVerified(), first.AccountEmail())
	}
	again, err := users.Provision(e.ctx, tenantA, account.Draft{Name: "Other", Email: "admin@example.test", Password: "another-long-password"})
	if err != nil {
		t.Fatalf("provisioning again: %v", err)
	}
	if again.AccountID() != first.AccountID() || again.AccountName() != "Admin" {
		t.Fatalf("provisioning an address twice returned id=%q name=%q, want the first account", again.AccountID(), again.AccountName())
	}
	found, err := users.Lookup(e.ctx, tenantA, " ADMIN@example.test")
	if err != nil || !found.IsVerified() {
		t.Fatalf("Lookup = verified %v (%v), want the verified account", err == nil && found.IsVerified(), err)
	}
	if n := e.count(`SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("provisioning twice stored %d rows, want one", n)
	}
	if got := len(e.events(first.AccountID())); got != 0 {
		t.Fatalf("provisioning stored %d events, want none", got)
	}
	for name, draft := range map[string]account.Draft{
		"both a password and a hash": {Email: "b@example.test", Password: password, PasswordHash: bcryptOf(t, password)},
		"neither":                    {Email: "c@example.test"},
		"a hash nothing can read":    {Email: "d@example.test", PasswordHash: "md5:5f4dcc3b5aa765d61d8327deb882cf99"},
		"a plain password as a hash": {Email: "e@example.test", PasswordHash: password},
		"a short password":           {Email: "f@example.test", Password: "short"},
	} {
		if _, err := users.Provision(e.ctx, tenantA, draft); err == nil {
			t.Errorf("a draft with %s was provisioned", name)
		}
	}
}

func lookupUnknown(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	e.register(users, tenantA, "Ana", "ana@example.test")
	if _, err := users.Lookup(e.ctx, tenantA, "nobody@example.test"); !errors.Is(err, account.ErrUserNotFound) {
		t.Fatalf("an unknown address answered %v, want ErrUserNotFound", err)
	}
	if _, err := users.Lookup(e.ctx, tenantB, "ana@example.test"); !errors.Is(err, account.ErrUserNotFound) {
		t.Fatalf("an address of another tenant answered %v, want ErrUserNotFound", err)
	}
}

func resetLinkSpent(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	captured := account.Fingerprint(registered.PasswordHash())

	if _, err := users.ResetPassword(e.ctx, tenantA, registered.AccountID(), "other@example.test", captured, "a-brand-new-password"); !errors.Is(err, account.ErrResetLinkSpent) {
		t.Fatalf("a link for another address answered %v, want ErrResetLinkSpent", err)
	}
	reset, err := users.ResetPassword(e.ctx, tenantA, registered.AccountID(), registered.AccountEmail(), captured, "a-brand-new-password")
	if err != nil {
		t.Fatalf("resetting: %v", err)
	}
	if account.Fingerprint(reset.PasswordHash()) == captured {
		t.Fatal("the reset account still has the fingerprint the link captured")
	}
	if _, err := users.ResetPassword(e.ctx, tenantA, registered.AccountID(), registered.AccountEmail(), captured, "a-third-new-password"); !errors.Is(err, account.ErrResetLinkSpent) {
		t.Fatalf("the same link twice answered %v, want ErrResetLinkSpent", err)
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", "a-brand-new-password", "127.0.0.1"); err != nil {
		t.Fatalf("signing in with the new password: %v", err)
	}
	if got := e.eventCount(registered.AccountID(), account.PasswordReset); got != 1 {
		t.Fatalf("the reset stored %d %s events, want one", got, account.PasswordReset)
	}

	if _, err := users.SetPassword(e.ctx, tenantA, "ana@example.test", "an-operator-password"); err != nil {
		t.Fatalf("setting the password from a command: %v", err)
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", "an-operator-password", "127.0.0.1"); err != nil {
		t.Fatalf("signing in with the operator's password: %v", err)
	}
}

func confirmPassword(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	if err := users.ConfirmPassword(e.ctx, subject(registered), password, "127.0.0.1"); err != nil {
		t.Fatalf("the right password answered %v", err)
	}
	if err := users.ConfirmPassword(e.ctx, subject(registered), "not-the-password-at-all", "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a wrong password answered %v, want auth.ErrInvalidCredentials", err)
	}
	ghost := auth.Subject{ID: registered.AccountID(), Tenant: tenantB}
	if err := users.ConfirmPassword(e.ctx, ghost, password, "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("the account's id in another tenant answered %v, want auth.ErrInvalidCredentials", err)
	}
	if err := users.ConfirmPassword(e.ctx, auth.Subject{}, password, "127.0.0.1"); err == nil {
		t.Fatal("an empty subject confirmed a password")
	}
}

func changePassword(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")

	var refused validation.Errors
	if _, err := users.ChangePassword(e.ctx, subject(registered), password, "short", "127.0.0.1"); !errors.As(err, &refused) || refused.First("password") == "" {
		t.Fatalf("a short new password answered %v, want a validation error on password", err)
	}
	// Seven characters in fourteen bytes is short, as the hashing counts it,
	// and is refused before the current password is checked.
	if _, err := users.ChangePassword(e.ctx, subject(registered), password, "ççççççç", "127.0.0.1"); !errors.As(err, &refused) || refused.First("password") == "" {
		t.Fatalf("a short new password of accented characters answered %v, want a validation error on password", err)
	}
	if _, err := users.ChangePassword(e.ctx, subject(registered), password, strings.Repeat("a", 129), "127.0.0.1"); !errors.As(err, &refused) || refused.First("password") == "" {
		t.Fatalf("a new password over the longest answered %v, want a validation error on password", err)
	}
	if _, err := users.ChangePassword(e.ctx, subject(registered), "not-the-password-at-all", "a-brand-new-password", "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a wrong current password answered %v, want auth.ErrInvalidCredentials", err)
	}
	changed, err := users.ChangePassword(e.ctx, subject(registered), password, "a-brand-new-password", "127.0.0.1")
	if err != nil {
		t.Fatalf("changing the password: %v", err)
	}
	if changed.PasswordHash() != e.text(`SELECT password FROM users WHERE id = ?`, registered.AccountID()) {
		t.Fatal("the account ChangePassword returned does not hold the stored hash")
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", password, "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("the old password after the change answered %v, want auth.ErrInvalidCredentials", err)
	}
	if got := e.eventCount(registered.AccountID(), account.PasswordChanged); got != 1 {
		t.Fatalf("the change stored %d %s events, want one", got, account.PasswordChanged)
	}
	for _, event := range e.events(registered.AccountID()) {
		if event.Name == account.PasswordChanged && event.AuthorizedBy != registered.AccountID() {
			t.Fatalf("the change was authorized by %q, want the account itself: a self-service write runs under the policy's Grant", event.AuthorizedBy)
		}
	}
}

func rename(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")

	var refused validation.Errors
	for _, name := range []string{"", "   ", strings.Repeat("a", 121)} {
		if _, err := users.Rename(e.ctx, subject(registered), name); !errors.As(err, &refused) || refused.First("name") == "" {
			t.Errorf("renaming to %q answered %v, want a validation error on name", name, err)
		}
	}
	renamed, err := users.Rename(e.ctx, subject(registered), "  Ana Maria ")
	if err != nil {
		t.Fatalf("renaming: %v", err)
	}
	if renamed.AccountName() != "Ana Maria" || e.text(`SELECT name FROM users WHERE id = ?`, registered.AccountID()) != "Ana Maria" {
		t.Fatalf("the renamed account is %q", renamed.AccountName())
	}
	if got := e.eventCount(registered.AccountID(), account.ProfileUpdated); got != 1 {
		t.Fatalf("the rename stored %d %s events, want one", got, account.ProfileUpdated)
	}
	if _, err := users.Rename(e.ctx, auth.Subject{ID: registered.AccountID(), Tenant: tenantB}, "Mallory"); err == nil {
		t.Fatal("a subject in another tenant renamed the account")
	}
	if _, err := users.Rename(e.ctx, auth.Subject{}, "Mallory"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("an empty subject answered %v, want auth.ErrForbidden", err)
	}
}

func closeAccount(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")

	if err := users.Close(e.ctx, subject(registered), "not-the-password-at-all", "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("closing with a wrong password answered %v, want auth.ErrInvalidCredentials", err)
	}
	if err := users.Close(e.ctx, subject(registered), password, "127.0.0.1"); err != nil {
		t.Fatalf("closing: %v", err)
	}
	if email := e.text(`SELECT email FROM users WHERE id = ?`, registered.AccountID()); email == "ana@example.test" {
		t.Fatal("the closed account still holds its address")
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", password, "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("signing in to a closed account answered %v, want auth.ErrInvalidCredentials", err)
	}
	e.register(users, tenantA, "Ana", "ana@example.test")

	var closed []string
	for _, event := range e.events(registered.AccountID()) {
		if event.Name == account.AccountClosed {
			closed = append(closed, event.Payload)
		}
	}
	if len(closed) != 1 {
		t.Fatalf("closing stored %d %s events, want one", len(closed), account.AccountClosed)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(closed[0]), &payload); err != nil {
		t.Fatalf("reading the payload: %v", err)
	}
	if payload["user_id"] != registered.AccountID() || payload["tenant_id"] != tenantA || payload["email"] != "" {
		t.Fatalf("the closed event carries %s, want the id and tenant and no address", closed[0])
	}
}

func publicNames(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	ana := e.register(users, tenantA, "Ana", "ana@example.test")
	bea := e.register(users, tenantA, "Bea", "bea@example.test")
	other := e.register(users, tenantB, "Carla", "carla@example.test")
	if _, err := users.Rename(e.ctx, subject(bea), "Beatriz"); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	names, err := users.PublicNames(e.ctx, subject(ana), []string{ana.AccountID(), bea.AccountID(), other.AccountID(), "nobody"})
	if err != nil {
		t.Fatalf("reading public names: %v", err)
	}
	if len(names) != 2 || names[ana.AccountID()] != "Ana" || names[bea.AccountID()] != "Beatriz" {
		t.Fatalf("public names = %v, want Ana and Beatriz and nobody of another tenant", names)
	}
	if empty, err := users.PublicNames(e.ctx, subject(ana), nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids answered %v, %v", empty, err)
	}
	if _, err := users.PublicNames(e.ctx, auth.Subject{}, []string{ana.AccountID()}); err == nil {
		t.Fatal("an unauthenticated subject read public names")
	}
}

func eventPayloads(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	stored := e.events(registered.AccountID())
	if len(stored) != 1 {
		t.Fatalf("registration stored %d events, want one", len(stored))
	}
	event := stored[0]
	want := `{"user_id":"` + registered.AccountID() + `","tenant_id":"tenant-a","email":"ana@example.test","name":"Ana"}`
	if event.Name != "auth.user.registered" || event.Aggregate != "user" || event.Payload != want {
		t.Fatalf("stored %s on %s with %s, want auth.user.registered on user with %s", event.Name, event.Aggregate, event.Payload, want)
	}
	if event.TenantID != tenantA || event.Action != string(account.ActionUserCreate) {
		t.Fatalf("the event was stored in %q under %q, want %s under %s", event.TenantID, event.Action, tenantA, account.ActionUserCreate)
	}
}
