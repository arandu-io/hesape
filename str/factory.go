package str

import "sync"

// The three factories Illuminate keeps as Str::$uuidFactory,
// Str::$ulidFactory and Str::$randomStringFactory. They stand in front of UUID,
// UUID7, OrderedUUID, ULID and Random so that a test can pin what those return.
var (
	factoryMu     sync.RWMutex
	uuidFactory   func() string
	ulidFactory   func() string
	randomFactory func(length int) string
)

// CreateUUIDsUsing answers for Str::createUuidsUsing. It sets the factory UUID,
// UUID7 and OrderedUUID answer from; passing nil puts them back to normal.
func CreateUUIDsUsing(factory func() string) {
	factoryMu.Lock()
	uuidFactory = factory
	factoryMu.Unlock()
}

// CreateUUIDsUsingSequence answers for Str::createUuidsUsingSequence. It hands
// out the sequence one value per call, in order.
//
// Once the sequence runs out whenMissing is called for every further value;
// passing nil for it falls back to generating a real UUID, which is what
// Illuminate's own default closure does.
func CreateUUIDsUsingSequence(sequence []string, whenMissing func() string) {
	CreateUUIDsUsing(sequenceFactory(sequence, whenMissing, func() string {
		return withoutUUIDFactory(UUID)
	}))
}

// FreezeUUIDs answers for Str::freezeUuids. Every UUID from here on is the one
// it returns.
//
// With a callback the freeze lasts only for the length of that call and is
// lifted afterwards, whatever the callback does -- Illuminate lifts it in a
// finally block, and this in a defer.
func FreezeUUIDs(callback ...func(uuid string)) string {
	uuid := UUID()
	CreateUUIDsUsing(func() string { return uuid })
	if len(callback) > 0 && callback[0] != nil {
		defer CreateUUIDsNormally()
		callback[0](uuid)
	}
	return uuid
}

// CreateUUIDsNormally answers for Str::createUuidsNormally. It drops the UUID
// factory, so UUID, UUID7 and OrderedUUID generate again.
func CreateUUIDsNormally() { CreateUUIDsUsing(nil) }

// CreateULIDsUsing answers for Str::createUlidsUsing. It sets the factory ULID
// answers from; passing nil puts it back to normal.
func CreateULIDsUsing(factory func() string) {
	factoryMu.Lock()
	ulidFactory = factory
	factoryMu.Unlock()
}

// CreateULIDsUsingSequence answers for Str::createUlidsUsingSequence. It hands
// out the sequence one value per call, in order, and falls to whenMissing after
// it -- or to a real ULID when whenMissing is nil.
func CreateULIDsUsingSequence(sequence []string, whenMissing func() string) {
	CreateULIDsUsing(sequenceFactory(sequence, whenMissing, func() string {
		return withoutULIDFactory(ULID)
	}))
}

// FreezeULIDs answers for Str::freezeUlids. Every ULID from here on is the one
// it returns, and with a callback the freeze lasts only for that call.
func FreezeULIDs(callback ...func(ulid string)) string {
	ulid := ULID()
	CreateULIDsUsing(func() string { return ulid })
	if len(callback) > 0 && callback[0] != nil {
		defer CreateULIDsNormally()
		callback[0](ulid)
	}
	return ulid
}

// CreateULIDsNormally answers for Str::createUlidsNormally. It drops the ULID
// factory, so ULID generates again.
func CreateULIDsNormally() { CreateULIDsUsing(nil) }

// CreateRandomStringsUsing answers for Str::createRandomStringsUsing. It sets
// the factory Random answers from; passing nil puts it back to normal.
func CreateRandomStringsUsing(factory func(length int) string) {
	factoryMu.Lock()
	randomFactory = factory
	factoryMu.Unlock()
}

// CreateRandomStringsUsingSequence answers for
// Str::createRandomStringsUsingSequence. It hands out the sequence one value
// per call, in order, ignoring the length that was asked for.
//
// Once the sequence runs out whenMissing is called with that length; passing
// nil for it falls back to generating a real random string.
func CreateRandomStringsUsingSequence(sequence []string, whenMissing func(length int) string) {
	next := 0
	if whenMissing == nil {
		whenMissing = func(length int) string {
			next++
			return withoutRandomFactory(func() string { return Random(length) })
		}
	}
	var mu sync.Mutex
	CreateRandomStringsUsing(func(length int) string {
		mu.Lock()
		defer mu.Unlock()
		if next < len(sequence) {
			v := sequence[next]
			next++
			return v
		}
		return whenMissing(length)
	})
}

// CreateRandomStringsNormally answers for Str::createRandomStringsNormally. It
// drops the random string factory, so Random generates again.
func CreateRandomStringsNormally() { CreateRandomStringsUsing(nil) }

// ResetFactoryState answers for Str::resetFactoryState. It drops all three
// factories at once, which is what a test tears down with.
//
// It is in the current Laravel and not in the clone this package was written
// against; see the package comment.
func ResetFactoryState() {
	CreateRandomStringsNormally()
	CreateULIDsNormally()
	CreateUUIDsNormally()
}

// sequenceFactory is the closure Illuminate builds for the two identifier
// sequences: the next value while the sequence lasts, and whenMissing after it.
func sequenceFactory(sequence []string, whenMissing, generate func() string) func() string {
	next := 0
	var mu sync.Mutex
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if next < len(sequence) {
			v := sequence[next]
			next++
			return v
		}
		if whenMissing != nil {
			return whenMissing()
		}
		next++
		return generate()
	}
}

// withoutUUIDFactory runs f with the UUID factory lifted and puts it back
// afterwards, which is how Illuminate's default whenMissing reaches a real UUID
// from inside the factory that replaced it.
func withoutUUIDFactory(f func() string) string {
	factoryMu.Lock()
	cached := uuidFactory
	uuidFactory = nil
	factoryMu.Unlock()
	defer func() {
		factoryMu.Lock()
		uuidFactory = cached
		factoryMu.Unlock()
	}()
	return f()
}

// withoutULIDFactory is withoutUUIDFactory for the ULID factory.
func withoutULIDFactory(f func() string) string {
	factoryMu.Lock()
	cached := ulidFactory
	ulidFactory = nil
	factoryMu.Unlock()
	defer func() {
		factoryMu.Lock()
		ulidFactory = cached
		factoryMu.Unlock()
	}()
	return f()
}

// withoutRandomFactory is withoutUUIDFactory for the random string factory.
func withoutRandomFactory(f func() string) string {
	factoryMu.Lock()
	cached := randomFactory
	randomFactory = nil
	factoryMu.Unlock()
	defer func() {
		factoryMu.Lock()
		randomFactory = cached
		factoryMu.Unlock()
	}()
	return f()
}
