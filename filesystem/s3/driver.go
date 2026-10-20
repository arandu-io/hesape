package s3

import "github.com/arandu-io/hesape/filesystem"

// DriverName is the driver a disk's filesystem.Config names to be built by
// [Driver]: Config{Driver: "s3", Bucket: ..., Endpoint: ...}.
const DriverName = "s3"

// Driver returns the creator that builds an object storage disk from its
// filesystem.Config, for filesystem.Disks.Extend:
//
//	disks.Extend(s3.DriverName, s3.Driver())
//
// After that line a disk the configuration describes with the "s3" driver is
// built like a local one, by Disks.Configure or Disks.Build, out of the
// bucket, endpoint, region, path style and credentials on its Config. Nothing
// else is read: the HTTP client is the instrumented one New installs, so every
// call to the bucket shows on the request timeline.
//
// What New refuses -- no endpoint, no bucket, no credentials -- is refused
// here as New words it; Disks.Configure puts the disk's name in front, so a
// boot that stops on a missing variable says which disk wanted it.
func Driver() filesystem.DriverCreator {
	return func(_ string, cfg filesystem.Config) (filesystem.Adapter, error) {
		adapter, err := New(Config{
			Endpoint:  cfg.Endpoint,
			Bucket:    cfg.Bucket,
			Region:    cfg.Region,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			PathStyle: cfg.PathStyle,
		})
		if err != nil {
			// A nil *AwsS3V3Adapter returned as an Adapter would be an
			// interface that is not nil.
			return nil, err
		}
		return adapter, nil
	}
}
