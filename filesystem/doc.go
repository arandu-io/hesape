// Package filesystem is the file contract: what a tenant uploaded, and where it
// went.
//
// It mirrors Illuminate\Filesystem. The files it answers to, in the clone at
// laravel_illuminate/filesystem:
//
//	AwsS3V3Adapter.php
//	Filesystem.php
//	FilesystemAdapter.php
//	FilesystemManager.php
//	FilesystemServiceProvider.php
//	LocalFilesystemAdapter.php
//	LockableFile.php
//	ReceiveFile.php
//	ServeFile.php
//	functions.php
//
// The shape, in one paragraph. A [Disk] is what an application calls, and every
// one of its methods takes an [auth.Grant]; an [Adapter] is what a driver
// implements, and it never hears of a tenant. Between the two sits [Key], which
// turns a Grant and a key into the one stored path that Grant may reach. That
// split is the whole design: tenant isolation is a property of this package,
// not of each driver remembering to ask.
//
// The contract lives in the collection and so does the driver that needs
// nothing installed: [LocalFilesystemAdapter] is a directory, and it is the one
// to develop against and to run a single machine on.
//
// Object storage is github.com/arandu-io/hesape/filesystem/s3 -- the same
// contract over the S3 protocol, with Cloudflare R2 as the default, and a module
// of its own so that a project storing on disk does not carry it. In Go there is
// no optional dependency, which is the whole reason that line exists (ADR 0048).
//
// A file is customer data, and a path without a tenant is a leak with a
// directory name -- which is why the Grant is on the collection's side of the
// split and not on the driver's, the same way it is for database.Repository and
// queue.Queue.
//
// There is no symlink into a document root. Publishing a storage directory that
// way makes every stored file world-readable by URL and turns authorization
// into "hope nobody guesses the name". Here a file is served by a route, and
// the route runs a Policy like any other -- see [Serve] for the serving half and
// [URLSigner] for the link that stands in for a session.
//
// The way in from a form is [Upload], checked against [UploadRules]. It is the
// "own contract" that validation refuses uploads in favour of: a file is not in
// url.Values, and validating one inside the form pipeline would be a second way
// to validate (RULE 9).
//
// # Three of Illuminate's methods are missing on purpose
//
// url() returns the permanent public address of a file, which only exists
// because a public disk does. There is none here, so there is no such address:
// the answer to "how do I link to this" is [URLSigner.TemporaryURL], and it
// expires.
//
// getVisibility() and setVisibility() are the same absence said twice. A stored
// file is reachable by a Policy or not at all, and a per-object flag saying
// otherwise would be a second authorization path (RULE 9).
//
// makeDirectory() has nothing to make. A directory here exists because a key
// under it does -- [LocalFilesystemAdapter] creates the parents on the way in,
// and an object store has no directories at all. Writing an empty marker object
// to stand for one would put a key in every listing that [Disk.Get] then answers
// ErrNotFound for.
package filesystem
