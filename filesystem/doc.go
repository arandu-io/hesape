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
// The contract lives in the collection and the drivers do not, for the same
// reason as database.Repository and queue.Queue: a file is customer data, and a
// path without a tenant is a leak with a directory name.
//
// There is no symlink into a document root. Publishing a storage directory that
// way makes every stored file world-readable by URL and turns authorization
// into "hope nobody guesses the name". Here a file is served by a route, and
// the route runs a Policy like any other -- see [Send] for the serving half and
// [URLSigner] for the link that stands in for a session.
//
// The way in from a form is [Upload], checked against [UploadRules]. It is the
// "own contract" that validation refuses uploads in favour of: a file is not in
// url.Values, and validating one inside the form pipeline would be a second way
// to validate (RULE 9).
package filesystem
