package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

const (
	colorOff  = "\033[0m"
	red       = "\033[31m"
	green     = "\033[32m"
	yellow    = "\033[33m"
	blue      = "\033[34m"
	purple    = "\033[35m"
	cyan      = "\033[36m"
	highlight = "\033[1m"
)

var colorList = []string{red, green, yellow, blue, purple, cyan}

// colorize returns the input string wrapped with a color code.
func colorize(colorID int, input string) string {
	color := colorList[colorID%len(colorList)]
	return fmt.Sprintf("%s%s%s", color, input, colorOff)
}

// execResult holds the SSH command execution result for a host.
type execResult struct {
	host      string
	err       error
	exitCode  int
	unmatched []string
}

func main() {
	var (
		cmd     string
		user    string
		jobs    int
		sshArgs string
		patStr  string
		quiet   bool
	)
	flag.StringVar(&cmd, "c", "echo ok", "Command to run on remote hosts")
	flag.StringVar(&user, "u", "", "SSH login username")
	flag.IntVar(&jobs, "j", 1, "Max concurrency for SSH executions")
	flag.StringVar(&sshArgs, "a", "-o BatchMode=yes", "Extra SSH arguments")
	flag.StringVar(&patStr, "p", `^\[.*\].* \[.*\]$`, "Regex filter for real-time line display")
	flag.BoolVar(&quiet, "q", false, "Quiet mode: only print matched lines")
	flag.Parse()

	hosts := flag.Args()
	if len(cmd) == 0 || len(user) == 0 || len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s -u <user> -c <command> [options] host1 host2 ...\n", os.Args[0])
		flag.Usage()
		os.Exit(1)
	}

	pat, err := regexp.Compile(patStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid regex pattern: %v\n", err)
		os.Exit(1)
	}

	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]execResult, len(hosts))

	fmt.Printf("%s%sCommand%s: [%s]\n", blue, highlight, colorOff, cmd)
	fmt.Printf("%s%sHosts%s: %v\n", blue, highlight, colorOff, hosts)

	for idx, host := range hosts {
		wg.Add(1)
		go func(i int, h string, colorID int) {
			defer wg.Done()
			sem <- struct{}{}
			results[i] = execHost(user, h, cmd, sshArgs, pat, colorID, quiet, &mu)
			<-sem
		}(idx, host, idx)
	}
	wg.Wait()

	var hasError bool
	for _, res := range results {
		if res.err != nil || res.exitCode != 0 {
			hasError = true
			fmt.Printf("%s[ERROR]%s Host: %s, %v\n", red, colorOff, res.host, res.err)
		}
	}

	if hasError {
		os.Exit(1)
	}
}

// execHost executes the given command over SSH for a single host.
func execHost(user, host, command, sshArgs string, pat *regexp.Regexp, colorID int, quiet bool, mu *sync.Mutex) execResult {
	sshBin := "ssh"
	args := []string{}
	if sshArgs != "" {
		args = strings.Fields(sshArgs)
	}
	args = append(args, fmt.Sprintf("%s@%s", user, host), command)
	cmd := exec.Command(sshBin, args...)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var unmatched []string
	if err := cmd.Start(); err != nil {
		return execResult{host: host, err: err, exitCode: 1}
	}

	var wg sync.WaitGroup
	process := func(reader *bufio.Reader) {
		defer wg.Done()
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(line, "\n")
				show := pat.MatchString(line)
				mu.Lock()
				if show {
					fmt.Printf("%s: %s\n", colorize(colorID, host), line)
				} else {
					unmatched = append(unmatched, line)
				}
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}

	wg.Add(2)
	go process(bufio.NewReader(stdout))
	go process(bufio.NewReader(stderr))
	wg.Wait()

	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if len(unmatched) > 0 && !quiet {
		mu.Lock()
		fmt.Printf(">>>>>>>>>>>>>>>> Unmatched output(%s):\n", colorize(colorID, host))
		for _, l := range unmatched {
			fmt.Println(l)
		}
		fmt.Println()
		mu.Unlock()
	}

	return execResult{
		host:      host,
		err:       err,
		exitCode:  exitCode,
		unmatched: unmatched,
	}
}
