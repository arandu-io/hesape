# Skills

Procedures an assistant follows when working on this collection.

They live in `.agents/skills/<name>/SKILL.md`, which is the path the coding
assistants read from — Cursor, Codex, Cline, Copilot, Gemini CLI, Amp, OpenCode,
Warp, Zed and the rest all look there. It is one directory rather than a file
per vendor, so a skill written once is read by whatever this project is being
written with.

Each file opens with frontmatter carrying a `name` and a `description`. The
description is what a tool reads to decide whether the skill is relevant, so it
names the situation you are in rather than the topic it covers.

| skill | when it fires |
| --- | --- |
| `hesape-package` | a new behaviour has no obvious home, or somebody proposes a new package |
| `hesape-dependency` | anything is about to enter the dependency graph: a Go module, a driver, a script, a stylesheet |
| `hesape-grant` | writing a method that reads or writes stored data, or asking why one needs a `Grant` |
| `hesape-testing` | writing, moving or running a test in any of the seven modules |

## Why these exist

This is a library, not an application. A change here compiles into every project
that imports the package it touches, and the three ways to get that wrong are
the three skills above: put the code where nobody will find it, pull something
into the graph that every consumer then carries, or write a data path that
nobody authorized.

The fourth is subtler. The suite is 3,750 test functions across 321 files, and
the value of a suite that size is that a failure names the behaviour that broke.
A test filed in the wrong package, or written against the implementation when
the contract was the point, subtracts from that.

The collection is also in nobody's training set, and it is close enough to
Laravel to invite the wrong guess. A model asked to add something here reaches
for a container, a facade, a macro or a contracts tree — all four exist as
packages that export nothing and argue why. `AGENTS.md` at the root lists them.

## Adding your own

A skill in this directory travels with the repository. Keep it a procedure
rather than a description: a file that says "read the documentation" never
changes what anybody does. Every command in one has to be a command that runs,
and every number in one has to be a number somebody measured.
