// Package remote answers to Illuminate\Remote, and answers that it is not built.
//
// Read this before looking for [Connection], [MultiConnection] or
// [RemoteManager]: none of them exists, on purpose, and the section titled
// "What you write instead" has the Go that replaces each of the five things they
// did. The rest of the file is the evidence for the decision, because a refusal
// without evidence is a refusal somebody will overturn by guessing.
//
// # What the component was
//
// Six files, twenty-nine public methods, measured against the clone at
// laravel_illuminate/remote (illuminate/remote at v5.0.0, the last tag before
// deletion; the repository was archived in 2015). It opened an SSH connection
// from PHP and drove it:
//
//	RemoteManager::into, connection, group, multiple, resolve  -- pick host(s)
//	Connection::define, task                                   -- name a command set
//	Connection::run, status, display                           -- run it, read it
//	Connection::put, putString, get, getString                 -- move files
//	SecLibGateway::connect, getAgent and the rest              -- the phpseclib driver
//
// Laravel deleted it in 5.1 and the successor is Envoy, a separate tool with a
// Blade-flavoured task file. Its neighbours in that product line -- Forge and
// Envoyer -- are refused by name in docs/06-escopo.md:161 and :164.
//
// # The driver is not the cost, and that is why this needed deciding
//
// The obvious reason to refuse -- a heavy dependency -- does not hold, and
// saying so is the point of this section. golang.org/x/crypto is the one
// dependency the root module declares (go.mod, and the CI rejects a second), and
// golang.org/x/crypto/ssh is inside it, along with ssh/agent for the agent
// SecLibGateway::getAgent talks to and ssh/knownhosts for host keys. An honest
// Connection::run is roughly:
//
//	client, err := ssh.Dial("tcp", host, config)   // no new dependency
//	session, err := client.NewSession()
//	out, err := session.CombinedOutput(command)
//
// So the question is the one the specification asked: with the driver free, is
// there anything left that hesape/process does not already give? Three
// candidates were weighed, one at a time.
//
// ## Running on several hosts -- no
//
// MultiConnection is a for loop over connections (MultiConnection.php:39-124 is
// the same five-line loop five times), and RemoteManager::multiple is the line
// that builds one out of a list of configured names. In Go that loop is
// [github.com/arandu-io/hesape/concurrency.Run], and what it loops over is a
// process. Nothing in it needs a type of its own.
//
// ## Moving files by SFTP -- no, and it is the one that would cost
//
// SecLibGateway::put and SecLibGateway::get use phpseclib's SFTP. There is no
// SFTP client in golang.org/x/crypto -- it stops at the SSH transport -- so
// this is the single surface that would need a dependency the CI rejects, or a
// seventh submodule under the ADR 0048 pattern whose only consumer is a
// component Laravel deleted. Writing SCP by hand instead is not a saving: it is
// a wire format with a known class of client-side path-traversal bugs
// (CVE-2019-6111), which is a poor thing to reimplement to avoid a dependency.
//
// ## A named task -- no
//
// Connection::define stores a string under a name and Connection::task looks it
// up (Connection.php:80-101). A named command in Go is a func, and the registry
// for one that a person types is [github.com/arandu-io/hesape/console.Command].
// A second registry keyed by string, holding shell fragments the compiler never
// sees, is the opposite of what this framework is for.
//
// # The two reasons that decide it
//
// First, RULE 9. docs/17-deploy.md is the deployment path: build a static
// binary, build an OCI image, let the platform roll it out, and run `aru
// migrate` as a pipeline step and never at boot (RULE 16). A task runner that
// opens a shell on a named production host is a second deployment path beside
// that one, and it is the path that does not leave a record of what ran.
//
// Second, RULE 14 and RULE 17. Every other way into a host in this framework
// carries an [github.com/arandu-io/hesape/auth.Grant] and filters by
// auth.Tenant. A remote shell carries neither and cannot: it is one surface on
// which the product's central claim -- that the compiler enforces the
// architecture -- would be false, and it would be false in the place with the
// most reach.
//
// docs/31-reorganizacao-hesape.md:122 and :408 already said "nada" for this
// component. This file is that decision with the weighing written down, so the
// next person does not have to redo it.
//
// # What you write instead
//
// Concrete Go for each of the five things the component did. The import is
// [github.com/arandu-io/hesape/process] throughout, and the transport is the ssh
// binary, which is on every machine that had a use for Illuminate\Remote.
//
// Connection::run -- one command on one host:
//
//	factory := process.NewFactory()
//	result, err := factory.Run(ctx, []string{"ssh", "deploy@web-1", "systemctl reload app"}, nil)
//	if err != nil {
//		return err // never started: no ssh binary, no route, deadline
//	}
//	if _, err := result.Throw(nil); err != nil {
//		return err // a non-zero exit, carrying both streams
//	}
//
// A failed exit is a result and not an error here; that is process's rule and
// Illuminate's. Connection::status is result.ExitCode, and Connection::display
// is what an OutputHandler passed in place of that nil does, line by line.
//
// MultiConnection::run, RemoteManager::group and RemoteManager::multiple -- the
// same command on several hosts. group reads one configured name and multiple
// takes a list of them; both end at the same slice of hosts, which here is the
// argument the caller already has:
//
//	hosts := []string{"web-1", "web-2", "web-3"} // the group, from config
//	tasks := make([]concurrency.Task[process.ProcessResult], 0, len(hosts))
//	for _, host := range hosts {
//		tasks = append(tasks, func(ctx context.Context) (process.ProcessResult, error) {
//			return factory.Run(ctx, []string{"ssh", "deploy@" + host, "systemctl reload app"}, nil)
//		})
//	}
//	results, err := concurrency.Run(ctx, tasks...)
//
// Illuminate ran the group in sequence. This runs it at once, and the first
// failure cancels the rest.
//
// Connection::put and Connection::putString -- send a file:
//
//	_, err := factory.Run(ctx, []string{"scp", "./app.env", "deploy@web-1:/etc/app/app.env"}, nil)
//
//	// putString: the contents are the input, and cat is the remote end.
//	_, err = factory.NewPendingProcess().
//		Input(contents).
//		Run(ctx, []string{"ssh", "deploy@web-1", "cat > /etc/app/app.env"}, nil)
//
// When the file is a customer's rather than the machine's, it is not this at all:
// it is [github.com/arandu-io/hesape/filesystem.Disk], which carries the Grant
// and the tenant prefix that scp does not.
//
// Connection::get and Connection::getString -- fetch a file:
//
//	result, err := factory.Run(ctx, []string{"ssh", "deploy@web-1", "cat /var/log/app/last"}, nil)
//	contents := result.Output() // getString
//
// Connection::define and Connection::task -- a named task:
//
//	// The task is a func, and the name is the one a person types.
//	reload := console.Command{
//		Name:        "hosts:reload",
//		Description: "reload the application on every web host",
//		Isolated:    "hosts:reload",
//		Run: func(ctx context.Context, o *console.IO) error {
//			// the group loop above
//		},
//	}
//
// Isolated is worth noticing: it is the lock that stops two people reloading at
// once, and Illuminate\Remote had nothing like it.
//
// # If this is ever revisited
//
// The trigger is not "somebody wants SSH". It is a first-party need that the ssh
// binary cannot serve -- host key pinning enforced in-process, or a fleet
// operation that has to report per-host state into the Collector. Until then the
// answer above is shorter than the component was, and it is testable: everything
// it does goes through process.Factory.Fake, which the SSH client would not.
//
// # The twelve names the measurement still asks about
//
// They are the accessors of the three types the section above refuses, and they
// are listed here by name so that the refusal covers the whole component rather
// than only the methods somebody happened to think of.
//
//	SecLibGateway::connected, getConnection, getHost, getPort, getNewKey,
//	nextLine, and GatewayInterface's connected and nextLine
//	    The phpseclib driver and the interface it satisfies. The transport is
//	    the ssh binary through hesape/process, which has no connection object
//	    to hand back and no line to read one at a time -- a process's output is
//	    a stream the caller ranges over.
//
//	SecLibGateway::getAgent
//	    A new System_SSH_Agent, so phpseclib can authenticate against the
//	    running ssh-agent. The ssh binary reads SSH_AUTH_SOCK itself, and
//	    process.Factory passes the environment through, so what this returns is
//	    a step nobody takes:
//
//	    _, err := factory.Run(ctx, []string{"ssh", "deploy@web-1", "true"}, nil)
//
//	    The name is worth keeping in the file because it is the one place the
//	    refusal could look like a dependency problem and is not:
//	    golang.org/x/crypto/ssh/agent is in the one module this repository
//	    already declares, so the agent could be spoken to in-process. It is not,
//	    for the two reasons above, not for want of a library.
//
//	RemoteManager::getDefaultConnection, setDefaultConnection
//	    Choosing among named hosts held in configuration. There is no registry
//	    of hosts here: the host is an argument to the command, from the caller's
//	    own configuration.
//
//	Connection::getGateway, getOutput, setOutput, RemoteManager::setOutput
//	    Swapping where the remote command's output is written. process takes an
//	    OutputHandler for the same job, and it is a parameter rather than
//	    something set on an object beforehand.
package remote
