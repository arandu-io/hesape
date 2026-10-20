package account

import "github.com/arandu-io/hesape/auth"

// The actions an application's user policy decides. They are constants so a
// misspelling cannot silently widen a policy, and they are the strings the
// application's policy already compares against.
const (
	ActionUserView        auth.Action = "user.view"
	ActionUserCreate      auth.Action = "user.create"
	ActionUserUpdate      auth.Action = "user.update"
	ActionUserDelete      auth.Action = "user.delete"
	ActionUserNamesPublic auth.Action = "user.names.public"
)

const (
	// ActionTwoFactorRead permits reading an enrolment, including its encrypted secret.
	ActionTwoFactorRead auth.Action = "user.two-factor.read"
	// ActionTwoFactorManage permits changing an enrolment and spending its codes.
	ActionTwoFactorManage auth.Action = "user.two-factor.manage"
)

// The events stored in the outbox, past-tense facts about one account. Every
// one has Aggregate "user", the account's id as AggregateID, and a UserEvent
// as its payload unless Hooks.Payload says otherwise.
const (
	UserRegistered           = "auth.user.registered"
	EmailVerified            = "auth.email.verified"
	PasswordReset            = "auth.password.reset"
	PasswordChanged          = "auth.password.changed"
	ProfileUpdated           = "auth.user.profile_updated"
	AccountClosed            = "auth.user.closed"
	TwoFactorEnabled         = "auth.two_factor.enabled"
	TwoFactorDisabled        = "auth.two_factor.disabled"
	RecoveryCodesRegenerated = "auth.two_factor.recovery_codes.regenerated"
	RecoveryCodeUsed         = "auth.two_factor.recovery_code.used"
)

// Aggregate is the aggregate every account event is about.
const Aggregate = "user"

// UserEvent is the payload account events carry: who, in which tenant, and
// nothing a consumer could sign in with.
type UserEvent struct {
	UserID string `json:"user_id"`
	Tenant string `json:"tenant_id"`
	Email  string `json:"email"`
	Name   string `json:"name,omitempty"`
}

// eventOf is the payload an account's event carries by default.
func eventOf(user Account) UserEvent {
	return UserEvent{UserID: user.AccountID(), Tenant: user.AccountTenant(), Email: user.AccountEmail(), Name: user.AccountName()}
}
