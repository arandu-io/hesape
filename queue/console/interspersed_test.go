package console_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/queue"
	queueconsole "github.com/arandu-io/hesape/queue/console"
	"github.com/arandu-io/hesape/queue/failed"
)

// askedProvider records what each command asked of the failed job list, and
// under which tenant, and holds nothing.
type askedProvider struct {
	failed.NullFailedJobProvider
	asked []string
}

func (p *askedProvider) Find(_ context.Context, g auth.Grant, id string) (failed.FailedJob, error) {
	p.asked = append(p.asked, "find "+auth.Tenant(g)+" "+id)
	return p.NullFailedJobProvider.Find(context.Background(), g, id)
}

func (p *askedProvider) Forget(_ context.Context, g auth.Grant, id string) (bool, error) {
	p.asked = append(p.asked, "forget "+auth.Tenant(g)+" "+id)
	return false, nil
}

func (p *askedProvider) IDs(_ context.Context, g auth.Grant, queue string) ([]string, error) {
	p.asked = append(p.asked, "ids "+auth.Tenant(g)+" "+queue)
	return nil, nil
}

// askedBatches records the batch a command looked up, and under which tenant.
// Every other method is the nil interface's, so a command that reached one
// would panic rather than pass.
type askedBatches struct {
	bus.BatchRepository
	asked []string
}

func (b *askedBatches) Find(_ context.Context, g auth.Grant, id string) (bus.Batch, error) {
	b.asked = append(b.asked, "find "+auth.Tenant(g)+" "+id)
	return bus.Batch{ID: id}, nil
}

// TestTheFailedJobCommandsReadTheirFlagsAfterTheIDs: flag.Parse stops at the
// first positional argument, so `queue:retry j-1 --tenant acme` once left the
// tenant unread and answered that it was required. Every order has to run the
// same command.
func TestTheFailedJobCommandsReadTheirFlagsAfterTheIDs(t *testing.T) {
	cases := []struct {
		name  string
		build func(*askedProvider, *askedBatches) console.Command
		args  []string
		want  []string
		batch []string
	}{
		{
			name: "retry, the tenant after the ids",
			build: func(p *askedProvider, _ *askedBatches) console.Command {
				return queueconsole.NewRetryCommand(p, queue.NewQueueManager(), nil).Command()
			},
			args: []string{"j-1", "j-2", "--tenant", "acme"},
			want: []string{"find acme j-1", "find acme j-2"},
		},
		{
			name: "retry, the flags around the ids",
			build: func(p *askedProvider, _ *askedBatches) console.Command {
				return queueconsole.NewRetryCommand(p, queue.NewQueueManager(), nil).Command()
			},
			args: []string{"--queue=mail", "j-1", "--tenant=acme"},
			want: []string{"ids acme mail", "find acme j-1"},
		},
		{
			name: "retry, everything after -- is an id, even what looks like a flag",
			build: func(p *askedProvider, _ *askedBatches) console.Command {
				return queueconsole.NewRetryCommand(p, queue.NewQueueManager(), nil).Command()
			},
			args: []string{"j-1", "--tenant", "acme", "--", "-j-2", "--tenant=other"},
			want: []string{"find acme j-1", "find acme -j-2", "find acme --tenant=other"},
		},
		{
			name: "forget, the tenant after the id",
			build: func(p *askedProvider, _ *askedBatches) console.Command {
				return queueconsole.NewForgetFailedCommand(p).Command()
			},
			args: []string{"j-1", "--tenant", "acme"},
			want: []string{"forget acme j-1"},
		},
		{
			name: "retry-batch, the tenant after the batch",
			build: func(p *askedProvider, b *askedBatches) console.Command {
				return queueconsole.NewRetryBatchCommand(b, p, queue.NewQueueManager()).Command()
			},
			args:  []string{"b-1", "--tenant", "acme"},
			batch: []string{"find acme b-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, b := &askedProvider{}, &askedBatches{}
			if out, err := run(t, tc.build(p, b), tc.args...); err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if got, want := strings.Join(p.asked, "; "), strings.Join(tc.want, "; "); got != want {
				t.Errorf("%v asked the failed job list %q, want %q", tc.args, got, want)
			}
			if got, want := strings.Join(b.asked, "; "), strings.Join(tc.batch, "; "); got != want {
				t.Errorf("%v asked the batches %q, want %q", tc.args, got, want)
			}
		})
	}
}

// TestAFlagAfterTheIDsIsStillRefusedWhenItIsUnknown: reading flags anywhere is
// not ignoring the ones nobody declared.
func TestAFlagAfterTheIDsIsStillRefusedWhenItIsUnknown(t *testing.T) {
	p := &askedProvider{}
	if _, err := run(t, queueconsole.NewForgetFailedCommand(p).Command(), "j-1", "--tenant", "acme", "--force"); err == nil {
		t.Fatal("queue:forget accepted a flag it does not declare")
	}
	if len(p.asked) != 0 {
		t.Errorf("a refused command still reached the list: %v", p.asked)
	}
}
