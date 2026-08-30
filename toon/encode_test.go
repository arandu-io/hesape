package toon_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/toon"
)

func TestEncodeHandlesTheRequestedGoValueShapes(t *testing.T) {
	type product struct {
		SKU   string  `json:"sku"`
		Name  string  `json:"name"`
		Price float64 `json:"price"`
		Stock int     `json:"stock"`
	}

	users := map[string]any{
		"users": []map[string]any{
			{"id": 1, "name": "Alice", "role": "admin"},
			{"id": 2, "name": "Bob", "role": "user"},
			{"id": 3, "name": "Charlie", "role": "user"},
		},
	}
	arrays := map[string]any{
		"tags":   []string{"reading", "gaming", "coding"},
		"scores": []int{85, 92, 78, 95},
	}

	tests := []struct {
		name    string
		input   any
		options []toon.EncodeOption
		want    string
	}{
		{
			name: "simple object",
			input: map[string]any{
				"id":     123,
				"name":   "Ada",
				"active": true,
			},
			want: "active: true\nid: 123\nname: Ada",
		},
		{
			name:  "tabular array",
			input: users,
			want:  "users[3]{id,name,role}:\n  1,Alice,admin\n  2,Bob,user\n  3,Charlie,user",
		},
		{
			name: "nested structures",
			input: map[string]any{
				"user": map[string]any{
					"id":   456,
					"name": "Eve",
					"preferences": map[string]any{
						"theme":    "dark",
						"language": "en",
					},
				},
			},
			want: "user:\n  id: 456\n  name: Eve\n  preferences:\n    language: en\n    theme: dark",
		},
		{
			name:  "primitive arrays",
			input: arrays,
			want:  "scores[4]: 85,92,78,95\ntags[3]: reading,gaming,coding",
		},
		{
			name: "structs",
			input: map[string]any{
				"products": []product{
					{SKU: "A001", Name: "Widget", Price: 19.99, Stock: 100},
					{SKU: "A002", Name: "Gadget", Price: 29.99, Stock: 50},
					{SKU: "A003", Name: "Doohickey", Price: 9.99, Stock: 200},
				},
			},
			want: "products[3]{sku,name,price,stock}:\n  A001,Widget,19.99,100\n  A002,Gadget,29.99,50\n  A003,Doohickey,9.99,200",
		},
		{
			name:    "tab delimiter",
			input:   users,
			options: []toon.EncodeOption{toon.WithDelimiter("\t")},
			want:    "users[3\t]{id\tname\trole}:\n  1\tAlice\tadmin\n  2\tBob\tuser\n  3\tCharlie\tuser",
		},
		{
			name: "mixed array",
			input: map[string]any{
				"items": []any{
					1,
					"text",
					true,
					map[string]any{"key": "value"},
				},
			},
			want: "items[4]:\n  - 1\n  - text\n  - true\n  - key: value",
		},
		{
			name: "time values",
			input: map[string]any{
				"event":     "User Login",
				"timestamp": time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			want: "event: User Login\ntimestamp: \"2025-01-15T10:30:00Z\"",
		},
		{
			name: "e-commerce order",
			input: map[string]any{
				"order": map[string]any{
					"id":     "ORD-12345",
					"date":   "2025-01-15",
					"status": "shipped",
					"customer": map[string]any{
						"name":  "John Doe",
						"email": "john@example.com",
					},
					"items": []map[string]any{
						{"sku": "WIDGET-1", "quantity": 2, "price": 19.99},
						{"sku": "GADGET-2", "quantity": 1, "price": 49.99},
					},
					"total": 89.97,
				},
			},
			want: "order:\n  customer:\n    email: john@example.com\n    name: John Doe\n  date: 2025-01-15\n  id: ORD-12345\n  items[2]{price,quantity,sku}:\n    19.99,2,WIDGET-1\n    49.99,1,GADGET-2\n  status: shipped\n  total: 89.97",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := toon.Encode(test.input, test.options...)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if got != test.want {
				t.Errorf("Encode = %q, want %q", got, test.want)
			}
		})
	}
}

func ExampleEncode() {
	value := struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}{ID: 123, Name: "Ada", Active: true}

	encoded, err := toon.Encode(value)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(encoded)
	// Output:
	// id: 123
	// name: Ada
	// active: true
}

func TestEncodeRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name   string
		option toon.EncodeOption
		want   string
	}{
		{name: "empty delimiter", option: toon.WithDelimiter(""), want: "delimiter"},
		{name: "unsupported delimiter", option: toon.WithDelimiter(";"), want: "delimiter"},
		{name: "multiple delimiters", option: toon.WithDelimiter(",,"), want: "delimiter"},
		{name: "zero indent", option: toon.WithIndent(0), want: "indent"},
		{name: "negative indent", option: toon.WithIndent(-1), want: "indent"},
		{name: "nil option", option: nil, want: "option"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := toon.Encode(map[string]any{"ok": true}, test.option)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Encode error = %v, want an error containing %q", err, test.want)
			}
		})
	}
}
