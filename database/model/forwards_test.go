package model

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// TestSimplePaginateOnTheModelIsAScopedPage: the common listing is one call on
// the model, and it is the same page the builder answers -- scoped by tenant,
// one row past the page to know whether there is another.
func TestSimplePaginateOnTheModelIsAScopedPage(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)}, query.Record{"id": int64(3)})

	page, err := model.SimplePaginate(context.Background(), grant(), 2, 1, pagination.Options{})
	if err != nil {
		t.Fatalf("SimplePaginate: %v", err)
	}
	sql := conn.last().SQL
	if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Errorf("SQL = %q, want the tenant scope a fresh query carries", sql)
	}
	if !strings.Contains(sql, "limit 3") {
		t.Errorf("SQL = %q, want perPage+1", sql)
	}
	if len(page.Items()) != 2 || !page.HasMorePages() {
		t.Errorf("page holds %d rows, more = %v", len(page.Items()), page.HasMorePages())
	}
}

// TestLatestOnTheModelOrdersByCreatedAtNewestFirst: the listing a generated
// service writes is Latest().SimplePaginate(...) off the model.
func TestLatestOnTheModelOrdersByCreatedAtNewestFirst(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)})

	if _, err := model.Latest().SimplePaginate(context.Background(), grant(), 10, 1, pagination.Options{}); err != nil {
		t.Fatalf("SimplePaginate: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `order by "created_at" desc`) {
		t.Errorf("SQL = %q, want newest first by created_at", sql)
	}
}

// TestOldestOnTheModelOrdersByTheGivenColumnOldestFirst: Oldest is Latest's
// mirror, and a column given replaces the created-at default.
func TestOldestOnTheModelOrdersByTheGivenColumnOldestFirst(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)})

	if _, err := model.Oldest("name").Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `order by "name" asc`) {
		t.Errorf("SQL = %q, want oldest first by name", sql)
	}
}

// TestDoesntExistOnTheModelIsAScopedCount: a seeder asks whether the table is
// empty off the model, and the question is the tenant's, not the table's.
func TestDoesntExistOnTheModelIsAScopedCount(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"aggregate": int64(0)})

	empty, err := model.DoesntExist(context.Background(), grant())
	if err != nil {
		t.Fatalf("DoesntExist: %v", err)
	}
	if !empty {
		t.Error("DoesntExist reported a row where the count was zero")
	}
	if sql := conn.last().SQL; !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Errorf("SQL = %q, want the tenant scope a fresh query carries", sql)
	}

	conn.queue(query.Record{"aggregate": int64(2)})
	if empty, err = model.DoesntExist(context.Background(), grant()); err != nil || empty {
		t.Errorf("DoesntExist = %v, %v over two rows, want false", empty, err)
	}
}
