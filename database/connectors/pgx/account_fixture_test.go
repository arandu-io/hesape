package pgx_test

// The application half of auth/account, as an application that starts from
// the skeleton has it: its users table and the two migrations that make it,
// its User model implementing account.Account, and its UserPolicy, unchanged
// from the skeleton but for the action constants. The contract suite runs
// against these, on this driver.

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/schema"
)

// accountUser is the application's User.
type accountUser struct {
	model.Model

	ID         string       `db:"id"`
	TenantID   string       `db:"tenant_id"`
	Name       string       `db:"name"`
	Email      string       `db:"email"`
	Password   string       `db:"password"`
	Roles      accountRoles `db:"roles"`
	VerifiedAt *time.Time   `db:"verified_at"`
	CreatedAt  time.Time    `db:"created_at"`
}

const accountAdmin = "admin"

var accountUserTable = model.NewTable(model.TableSpec{
	Name:            "users",
	New:             func() model.Entity { return new(accountUser) },
	UniqueIDs:       true,
	UpdatedAtColumn: model.NoColumn,
})

// accountUsers is what aru model:build generates as models.Users(db).Base().
func accountUsers(db model.DB) *model.Builder { return accountUserTable.Query(db) }

func (u *accountUser) AccountID() string     { return u.ID }
func (u *accountUser) AccountTenant() string { return u.TenantID }
func (u *accountUser) AccountEmail() string  { return u.Email }
func (u *accountUser) AccountName() string   { return u.Name }
func (u *accountUser) PasswordHash() string  { return u.Password }
func (u *accountUser) IsVerified() bool      { return u.VerifiedAt != nil && !u.VerifiedAt.IsZero() }

func (u *accountUser) subject() auth.Subject {
	return auth.Subject{ID: u.ID, Tenant: u.TenantID, Roles: append([]string(nil), u.Roles...), Verified: u.IsVerified()}
}

// MarshalJSON keeps the password hash out, as the skeleton's does.
func (u *accountUser) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID    string   `json:"id"`
		Email string   `json:"email"`
		Roles []string `json:"roles"`
	}{u.ID, u.Email, u.Roles})
}

var _ account.Account = (*accountUser)(nil)

// accountRoles is a JSON array of text in one portable column.
type accountRoles []string

func (r accountRoles) Value() (driver.Value, error) {
	if r == nil {
		r = accountRoles{}
	}
	b, err := json.Marshal([]string(r))
	return string(b), err
}

func (r *accountRoles) Scan(src any) error {
	var text []byte
	switch v := src.(type) {
	case nil:
		*r = accountRoles{}
		return nil
	case string:
		text = []byte(v)
	case []byte:
		text = v
	default:
		return fmt.Errorf("user: roles column holds %T", src)
	}
	var roles []string
	if len(text) > 0 {
		if err := json.Unmarshal(text, &roles); err != nil {
			return err
		}
	}
	if roles == nil {
		roles = []string{}
	}
	*r = roles
	return nil
}

// accountPolicy is the skeleton's UserPolicy.
type accountPolicy struct{}

func (accountPolicy) Can(_ context.Context, subject auth.Subject, action auth.Action, user *accountUser) error {
	if action == account.ActionUserNamesPublic {
		if subject.Tenant == "" || user.TenantID == "" || user.TenantID != subject.Tenant {
			return fmt.Errorf("public user names require the reader's tenant")
		}
		return nil
	}
	if user.TenantID != "" && user.TenantID != subject.Tenant {
		return fmt.Errorf("resource belongs to another tenant")
	}
	switch action {
	case account.ActionUserView, account.ActionUserUpdate:
		if subject.ID == user.ID || subject.HasRole(accountAdmin) {
			return nil
		}
	case account.ActionUserCreate:
		if subject.HasRole(accountAdmin) || subject.IsGuest() && len(user.Roles) == 0 {
			return nil
		}
	case account.ActionUserDelete:
		if subject.HasRole(accountAdmin) {
			return nil
		}
	}
	return fmt.Errorf("insufficient role for %s", action)
}

// accountUserMigrations are the skeleton's two users migrations, verbatim.
func accountUserMigrations() []migrations.Migration {
	return []migrations.Migration{createAccountUsers{}, addAccountNameAndVerification{}}
}

type createAccountUsers struct{ migrations.BaseMigration }

func (createAccountUsers) GetName() string { return "20260729_0001_create_users" }
func (createAccountUsers) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, "users", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("email")
		table.Text("password")
		table.Text("roles")
		table.Timestamp("created_at")
		table.Unique([]string{"tenant_id", "email"})
		table.Index([]string{"tenant_id", "created_at", "id"}, "users_tenant_created_idx")
	})
}

type addAccountNameAndVerification struct{ migrations.BaseMigration }

func (addAccountNameAndVerification) GetName() string {
	return "20260809_0002_add_name_and_verification_to_users"
}
func (addAccountNameAndVerification) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, "users", func(table *schema.Blueprint) {
		table.String("name").Nullable()
		table.Timestamp("verified_at").Nullable()
	})
}
