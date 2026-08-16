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
// # Two file APIs, and the line between them
//
// [Disk] is customer data: every method takes an [auth.Grant] and every path it
// builds starts with a tenant. [Filesystem] is the application's own files --
// stubs, compiled views, session files, cache entries -- and takes plain paths,
// exactly as os.ReadFile does. Illuminate has the same two classes for the same
// reason; the failure worth naming is putting an upload through the second one,
// where there is no prefix and therefore no isolation.
//
// [LockableFile] is what makes the second one safe to share between processes,
// and [Filesystem.SharedGet] and [Filesystem.Put] with lock are the pattern.
//
// # What is not ported, and why
//
// Twelve public methods of the component have no name here. Each one, with the
// ADR 0056 reason number:
//
//	Filesystem::getRequire and Filesystem::requireOnce -- reason 1: their bodies
//	    are `require $path` and `require_once $path`, which load and execute PHP
//	    at run time. A Go program is linked before it starts; the file a caller
//	    would have required is a package it imports, and there is nothing here
//	    that can be made to do the same thing.
//	FilesystemServiceProvider::register, ::boot and ::setApplication -- reason 2:
//	    they bind 'files' and 'filesystem' into the container, register the disk
//	    names out of config('filesystems'), and publish the storage symlink. A
//	    Disk here is constructed and passed, and there is no symlink at all --
//	    the paragraph above says why.
//	FilesystemManager::createS3Driver, ::createFtpDriver and ::createSftpDriver
//	    -- reason 3: three drivers this ecosystem does not carry in the
//	    collection. S3 is github.com/arandu-io/hesape/filesystem/s3, a module of
//	    its own (ADR 0048), and it is [s3.New] rather than a driver string.
//	    FTP and SFTP are league/flysystem adapters with no Go equivalent worth
//	    keeping, and no second way to reach a file is added for them (RULE 9).
//	AwsS3V3Adapter::getClient -- reason 3: it hands back the aws-sdk-php client
//	    so that a caller can make a call the adapter does not expose. The S3
//	    module speaks the protocol over net/http and has no client object to
//	    hand back.
//	Storage::fake and Storage::persistentFake -- reason 2, both: each one
//	    replaces the named disk inside the facade's manager with a local one
//	    rooted at storage/framework/testing/disks, so that code reaching for
//	    Storage::disk('s3') gets the local one instead. There is no facade and
//	    no manager to reach into (ADR 0002), so a test builds the disk it wants
//	    and passes it, the same way it passes every other collaborator:
//
//	        adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
//	        if err != nil {
//	            t.Fatal(err)
//	        }
//	        disk := filesystem.NewDisk("local", adapter)
//
//	        archiveInvoice(ctx, g, disk)
//
//	        disk.AssertExists(ctx, t, g, "invoices/2026-114.pdf")
//
//	    t.TempDir() is what makes it Storage::fake rather than a real disk: the
//	    directory is made for this test and removed when it ends, so two tests
//	    running in parallel cannot see each other's files -- which the shared
//	    root under storage/ is why the PHP has to suffix it with the parallel
//	    testing token. persistentFake is the same two lines with a directory of
//	    your own in place of t.TempDir(), which is the whole of what the second
//	    method changes. The assertions are already here, on [Disk]:
//	    AssertExists, AssertMissing, AssertCount and AssertDirectoryEmpty.
//
// join_paths() is here, as [JoinPaths]. It is a free function in the PHP's
// functions.php, and Go has no snake_case, which is the mechanical change ADR
// 0044 allows for an initial and says here.
//
// # url(), getVisibility() and makeDirectory()
//
// All three are here, and each of them means less than its name suggests:
//
//   - [Disk.URL] is the permanent public address of a file. It carries no
//     authorization at all, so it answers [ErrNoURL] unless the disk was
//     configured with one. The answer for a tenant's file is
//     [Disk.TemporaryURL] or [URLSigner.TemporaryURL], which expire.
//   - [Disk.GetVisibility] and [Disk.SetVisibility] report and set what the
//     STORE will hand out to somebody who never came through the application.
//     They do not replace a Policy: every read here still takes a Grant
//     (RULE 17).
//   - [Disk.MakeDirectory] makes a directory on a driver that has them, and
//     succeeds without doing anything on one that does not -- an object store
//     has no directories, and a marker object standing in for one would put a
//     key in every listing that [Disk.Get] then answers [ErrNotFound] for.
package filesystem
