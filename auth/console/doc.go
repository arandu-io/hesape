// Package console holds the console command authentication ships with.
//
// There is one: [ClearResetsCommand] deletes the password reset tokens that
// have expired. It is a scheduled sweep, not a boot step -- a table that only
// grows is a table that eventually holds every reset anybody ever asked for.
//
// [NewClearResetsCommand] takes the broker factory as an argument, and the
// interfaces it needs are declared here rather than imported from
// hesape/auth/passwords: this package needs three methods of it, and an
// interface is what lets the command be built and tested on its own.
package console
