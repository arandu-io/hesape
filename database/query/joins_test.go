package query_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// The difference between a join's "on" and its "where" is the one people move a
// condition across and lose: on names a column on the right, where binds a
// value. Both spellings are pinned here.

func TestJoinWhereBindsTheRightHandSide(t *testing.T) {
	b := mysqlBuilder().JoinWhere("contacts", "contacts.user_id", "=", "users.id", "")
	assertSQL(t, b, "select * from `users` inner join `contacts` on `contacts`.`user_id` = ?")
	assertBindings(t, b, "users.id")
}

func TestJoinNamesTheColumnOnTheRight(t *testing.T) {
	b := mysqlBuilder().Join("contacts", "contacts.user_id", "=", "users.id")
	assertSQL(t, b, "select * from `users` inner join `contacts` on `contacts`.`user_id` = `users`.`id`")
	assertBindings(t, b)
}

func TestLeftAndRightJoinWhereCarryTheirType(t *testing.T) {
	b := mysqlBuilder().
		LeftJoinWhere("contacts", "contacts.kind", "=", "billing").
		RightJoinWhere("orders", "orders.state", "=", "paid")

	assertSQL(t, b, "select * from `users` "+
		"left join `contacts` on `contacts`.`kind` = ? "+
		"right join `orders` on `orders`.`state` = ?")
	assertBindings(t, b, "billing", "paid")
}

// MySQL is the engine that has a straight join, and its grammar drops the
// "join" word because the type already carries it.
func TestStraightJoinIsSpelledWithoutTheJoinWord(t *testing.T) {
	b := mysqlBuilder().StraightJoin("contacts", "contacts.user_id", "=", "users.id")
	assertSQL(t, b, "select * from `users` straight_join `contacts` on `contacts`.`user_id` = `users`.`id`")
}

// Every other engine is handed the type as written and refuses it, which is
// what the grammar's SupportsStraightJoins reports.
func TestStraightJoinIsSpelledWithTheJoinWordElsewhere(t *testing.T) {
	b := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil).From("users").
		StraightJoin("contacts", "contacts.user_id", "=", "users.id")
	if got := b.ToSQL(); !strings.Contains(got, "straight_join join") {
		t.Errorf("the statement is\n  %s\nwant the type compiled through for the engine to reject", got)
	}
}

func TestJoinLateralCompilesWithoutAnOnClause(t *testing.T) {
	sub := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("invoices").Limit(1)
	b := mysqlBuilder().LeftJoinLateral(sub, "latest")

	assertSQL(t, b, "select * from `users` left join lateral (select * from `invoices` limit 1) as `latest` on true")
}

// An engine without lateral joins must not compile the statement into
// something that runs: dropping the join would change which rows come back.
func TestALateralJoinOnAnEngineWithoutOneDoesNotCompile(t *testing.T) {
	sub := query.NewBuilder(nil, grammars.NewSQLiteGrammar(), nil).From("invoices")
	b := query.NewBuilder(nil, grammars.NewSQLiteGrammar(), nil).From("users").
		JoinLateral(sub, "latest", "inner")

	got := b.ToSQL()
	if !strings.Contains(got, "unsupported join") {
		t.Errorf("the statement is\n  %s\nwant a fragment the engine refuses", got)
	}
	if !strings.Contains(got, "lateral joins") {
		t.Errorf("the statement does not carry the reason:\n%s", got)
	}
}

func TestJoinSubCompilesTheSubqueryIntoTheJoin(t *testing.T) {
	sub := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("invoices").Where("total", ">", 100)
	b := mysqlBuilder().JoinSub(sub, "i", "i.user_id", "=", "users.id", "left", false)

	assertSQL(t, b, "select * from `users` "+
		"left join (select * from `invoices` where `total` > ?) as `i` on `i`.`user_id` = `users`.`id`")
	assertBindings(t, b, 100)
}

func TestCrossJoinSubHasNoCondition(t *testing.T) {
	sub := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("plans")
	b := mysqlBuilder().CrossJoinSub(sub, "p")

	assertSQL(t, b, "select * from `users` cross join (select * from `plans`) as `p`")
}

func TestFromSubAndSelectSubCompileWhereTheyWereWritten(t *testing.T) {
	spend := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("invoices").SelectRaw("sum(total)")
	recent := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).From("users").Where("state", "=", "active")

	b := mysqlBuilder().FromSub(recent, "u").SelectSub(spend, "spend").Where("u.id", "=", 7)

	assertSQL(t, b, "select (select sum(total) from `invoices`) as `spend` "+
		"from (select * from `users` where `state` = ?) as `u` where `u`.`id` = ?")
	// The select segment is read before the from segment, and both before the
	// where: the order of the placeholders is the order of the values.
	assertBindings(t, b, "active", 7)
}

func TestSelectExpressionBindsNothing(t *testing.T) {
	b := mysqlBuilder().Select("id").SelectExpression(query.Raw("1 + 1"), "two")
	assertSQL(t, b, "select `id`, (1 + 1) as `two` from `users`")
	assertBindings(t, b)
}
