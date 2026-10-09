// Package publish writes a module's files into a project and can be run again
// without eating what the project did to them.
//
// It is deliberately ignorant of what it is publishing. It takes a tree, a root
// to write it under and a lock recording what an earlier run wrote, and it
// knows nothing about views, configuration or migrations -- the caller does,
// and the caller is the one place where that vocabulary belongs.
//
// # The three guarantees
//
// Publishing twice does not change a file. What Plan reports as unchanged is
// unchanged: the same tree over the same project produces no write at all, so
// running the command because you are not sure whether you ran it is free.
//
// What would happen is reported before it happens. Plan touches nothing; Apply
// writes what Plan returned. A publication nobody could look at first is one
// people run in a branch and read afterwards.
//
// A customization is never overwritten in silence. Whatever is written between
// the arandu:begin custom markers is carried into the new file, and a file
// changed outside them is reported as a conflict rather than replaced -- the
// lock is what makes the second half possible, because "the file is there" does
// not distinguish the file we wrote from the one somebody rewrote.
package publish
