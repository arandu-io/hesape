package connections

// PacksPhpRedisValues reports what the driver does to a value on its way to the
// server, and prepares values for the eval command.
//
// A driver that serialized or compressed on the way out would need every value
// handed to a script treated the same way by hand, or the script would see
// bytes the rest of the application cannot read back.
//
// # Here it is the "none" case, permanently
//
// go-redis has no serializer option and no compression option: what a caller
// passes is what goes on the wire, and what comes back is what was stored. So
// every question this type answers is answered no, and Pack is the identity.
//
// That is not a stub. It is the honest reading of the same questions against a
// driver that made a different choice, and it is why the answers are constants
// rather than configuration: there is no setting that would change them, and a
// type that pretended otherwise would be a second way to encode a value.
// Encoding belongs to the caller -- cache.Repository serializes, and it is the
// only thing in this collection that does.
//
// It is embedded by Connection, so the methods are reachable on a connection.
type PacksPhpRedisValues struct{}

// Pack prepares the given values to be used with the eval command.
func (PacksPhpRedisValues) Pack(values []string) []string { return values }

// WithoutSerializationOrCompression executes callback with serialization and
// compression turned off.
//
// Both are always off, so it calls the callback and returns what it returned --
// and it exists rather than being dropped because a caller who wraps a read in
// it is
// stating something true about the read, and the statement should keep
// compiling if this adapter ever gains an encoder.
func (PacksPhpRedisValues) WithoutSerializationOrCompression(callback func() error) error {
	return callback()
}

// Serialized reports whether serialization is enabled. It is not.
func (PacksPhpRedisValues) Serialized() bool { return false }

// Compressed reports whether compression is enabled. It is not.
func (PacksPhpRedisValues) Compressed() bool { return false }

// LzfCompressed reports whether LZF compression is enabled. It is not.
func (PacksPhpRedisValues) LzfCompressed() bool { return false }

// ZstdCompressed reports whether Zstd compression is enabled. It is not.
func (PacksPhpRedisValues) ZstdCompressed() bool { return false }

// Lz4Compressed reports whether LZ4 compression is enabled. It is not.
func (PacksPhpRedisValues) Lz4Compressed() bool { return false }
