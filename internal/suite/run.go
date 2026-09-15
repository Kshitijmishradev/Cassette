package suite

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Kshitijmishradev/cassette/internal/config"
	"github.com/Kshitijmishradev/cassette/internal/diff"
	"github.com/Kshitijmishradev/cassette/internal/env"
	"github.com/Kshitijmishradev/cassette/internal/replay"
	"github.com/Kshitijmishradev/cassette/internal/safety"
	"github.com/Kshitijmishradev/cassette/internal/tape"
)

// Options configures a suite run.
type Options struct {
	// Agent overrides the command recorded in each manifest. Empty reuses
	// what was recorded, which is the default because the point is to change
	// one thing at a time.
	Agent []string

	Hermetic bool
	Jobs     int

	Config     config.Config
	Classifier *safety.Classifier

	// Stdout and Stderr receive the agent's own output. Nil discards it,
	// which is what a suite run wants: fifty agents writing to one terminal
	// produces nothing anyone can read.
	Stdout, Stderr interface{ Write([]byte) (int, error) }
}

// Result is what one case produced.
type Result struct {
	Case     Case
	Diffs    []diff.Diff
	Reports  []replay.Report
	Verdict  diff.Verdict
	Refused  int
	Duration time.Duration

	// Output is the agent's captured stdout and stderr, kept so a failing
	// case can explain itself without the whole suite having been noisy.
	Output []byte

	Err error
}

// Failed reports a case that could not be run at all, as opposed to one that
// ran and found a behavioral difference. Conflating the two would make a
// broken harness look like a code regression.
func (r Result) Failed() bool { return r.Err != nil }

// RunCase replays one recorded run and diffs it against its recording.
func RunCase(ctx context.Context, c Case, opts Options) Result {
	started := time.Now()
	res := Result{Case: c}

	agentArgv := opts.Agent
	if len(agentArgv) == 0 {
		agentArgv = c.Manifest.Agent
	}
	if len(agentArgv) == 0 {
		res.Err = fmt.Errorf("no agent command given and none recorded in the manifest")
		return res
	}

	// Stale reports from an earlier run would otherwise be collected as if
	// they belonged to this one.
	if err := replay.CleanReports(c.Dir); err != nil {
		res.Err = err
		return res
	}

	var captured bytes.Buffer
	agent := exec.CommandContext(ctx, agentArgv[0], agentArgv[1:]...)
	agent.Stdout = &captured
	agent.Stderr = &captured
	agent.Env = append(os.Environ(),
		env.ModeVar+"="+string(env.ModeReplay),
		env.TapeVar+"="+abs(c.Dir),
		env.RunIDVar+"="+strconv.FormatInt(time.Now().UnixNano(), 36),
	)
	if opts.Hermetic {
		agent.Env = append(agent.Env, env.HermeticVar+"=1")
	}

	runErr := agent.Run()
	res.Output = captured.Bytes()
	res.Duration = time.Since(started)

	// A non-zero agent exit is not by itself a failure. An agent whose task
	// was to fix a failing test may legitimately exit non-zero, and what we
	// are measuring is its trajectory, not its exit code.
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); !ok {
			res.Err = fmt.Errorf("running agent: %w", runErr)
			return res
		}
	}

	reports, err := replay.CollectReports(c.Dir)
	if err != nil {
		res.Err = err
		return res
	}
	if len(reports) == 0 {
		res.Err = fmt.Errorf("no shim reported in; is `cassette wrap` in the agent's MCP config?")
		return res
	}
	res.Reports = reports

	for _, rep := range reports {
		res.Refused += rep.Refused

		rd, err := tape.Open(filepath.Join(c.Dir, rep.Tape+".cas"))
		if err != nil {
			res.Err = err
			return res
		}
		d := diff.Compare(
			diff.FromTape("recorded", rd, opts.Classifier),
			diff.FromReport("this run", rep, opts.Classifier),
		)
		rd.Close()

		res.Diffs = append(res.Diffs, d)
		if d.Verdict > res.Verdict {
			res.Verdict = d.Verdict
		}
	}
	return res
}

// Run replays every case, in parallel.
//
// Ordering of the returned slice matches the input, not completion order, so
// the report reads the same whatever the scheduler did. A suite report whose
// row order changed between runs would be miserable to diff.
func Run(ctx context.Context, cases []Case, opts Options, onDone func(Result)) []Result {
	jobs := opts.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if jobs > len(cases) {
		jobs = len(cases)
	}
	if jobs < 1 {
		jobs = 1
	}

	results := make([]Result, len(cases))
	sem := make(chan struct{}, jobs)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, c := range cases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := RunCase(ctx, c, opts)
			results[i] = r

			if onDone != nil {
				// Serialized so progress lines from parallel workers do not
				// interleave into gibberish.
				mu.Lock()
				onDone(r)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results
}

func abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}
