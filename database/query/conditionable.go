package query

// When answers Illuminate\Support\Traits\Conditionable::when, the trait the
// PHP builder uses.
//
// Three mechanical changes, all of them forced by Go. The condition is a bool
// rather than PHP's truthy mixed, because there is no truthiness to lean on and
// the caller writing the expression is the one who knows what makes it true.
// The callback mutates the builder and returns nothing, rather than returning a
// value the PHP then coalesces with $this -- every method on Builder already
// mutates and returns the receiver, so a callback that returned something would
// be a second convention. And the zero and one argument forms are absent, since
// they answer with a HigherOrderWhenProxy built on __get and __call.
//
// A nil callback on a true condition leaves the builder untouched instead of
// failing, which is what Collection.When does with the same argument.
//
//	db.Table("posts").
//		When(published, func(q *query.Builder) { q.Where("published", true) }, nil).
//		OrderBy("id")
func (b *Builder) When(condition bool, callback, otherwise func(*Builder)) *Builder {
	if condition {
		if callback != nil {
			callback(b)
		}
		return b
	}
	if otherwise != nil {
		otherwise(b)
	}
	return b
}

// Unless answers Illuminate\Support\Traits\Conditionable::unless: When with the
// condition negated, which is how the PHP defines it too.
func (b *Builder) Unless(condition bool, callback, otherwise func(*Builder)) *Builder {
	return b.When(!condition, callback, otherwise)
}
