package account

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// Migrations returns the migration that creates the second factor's two
// tables. The users table is the application's, and so is its migration.
//
// Its name is the identity an application that created these tables itself
// has already recorded, so an application that moves to this package and
// registers it finds it applied and runs nothing. Run it with the rest of the
// application's migrations, as a step of the deploy, never when the process
// starts.
func Migrations() []migrations.Migration {
	return []migrations.Migration{createTwoFactor{}}
}

// createTwoFactor creates the enrolment table and the recovery-code table.
type createTwoFactor struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order: the users
// table it hangs off is created before it.
func (createTwoFactor) GetName() string { return "20260828_0003_create_two_factor" }

// Up creates one enrolment per account and its single-use recovery codes.
func (createTwoFactor) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, twoFactorTableName, func(table *schema.Blueprint) {
		table.String("user_id").Primary()
		table.String("tenant_id")
		table.Text("secret")
		table.Timestamp("confirmed_at").Nullable()
		table.BigInteger("last_used_step")
		table.Timestamp("created_at")
	}); err != nil {
		return err
	}

	return conn.Schema().Create(ctx, recoveryCodeTableName, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("user_id")
		table.Text("code_hash")
		table.Timestamp("used_at").Nullable()
		table.Timestamp("created_at")

		table.Index([]string{"tenant_id", "user_id"}, "user_recovery_codes_owner_idx")
	})
}

// Down drops the recovery codes before the enrolment table they belong to.
func (createTwoFactor) Down(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().DropIfExists(ctx, recoveryCodeTableName); err != nil {
		return err
	}
	return conn.Schema().DropIfExists(ctx, twoFactorTableName)
}

var _ migrations.ReversibleMigration = createTwoFactor{}
