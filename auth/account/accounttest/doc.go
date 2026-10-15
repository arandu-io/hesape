// Package accounttest is the contract suite an application runs to prove the
// account package against its own database and its own user model.
//
// The package's rules are written once, but three things they depend on are
// the application's: the users table, the user model that implements
// account.Account, and the user policy. A user model whose AccountEmail reads
// the wrong field, a policy that does not let a guest register, a users table
// whose email is not unique per tenant -- each compiles, and each breaks
// sign-in. This suite is what notices.
//
// An application runs it in one test, against a fresh database per subtest:
//
//	func TestAccountContract(t *testing.T) {
//		accounttest.Run(t, func(t *testing.T) accounttest.Fixture[*models.User] {
//			db := openAMigratedDatabaseForThisTest(t)
//			return accounttest.Fixture[*models.User]{
//				DB:     db,
//				Query:  func() *model.Builder { return models.Users(db).Base() },
//				Policy: policies.UserPolicy{},
//			}
//		})
//	}
//
// The database the factory returns has the application's users table, the
// outbox and the tables account.Migrations creates, and nothing in them. The
// policy has to let a guest of a tenant create an account with no privileges,
// let a subject view and update their own account, and let a subject read the
// public names of their own tenant: that is what every registration, settings
// screen and author line asks of it, and the suite exercises all three.
//
// Rows of the second factor are seeded with statements written here, because
// a factor that is already enrolled is the state most of the challenge is
// about, and enrolling through the API spends the time step the challenge
// then needs. Those statements name the tables and columns the migration
// creates, and nothing of the application's.
//
// It is named for net/http/httptest: a package whose name ends in "test" is a
// package that imports testing, and every consumer of this one is a test.
package accounttest
