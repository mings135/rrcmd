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

// tail returns the last n lines of the input slice.
func tail(lines []string, num int) []string {
	start := max(0, len(lines)-num)

	return lines[start:]
}

type Result struct {
	host string
	code int
}

type Config struct {
	user   string
	bin    string
	jobs   int
	args   string
	path   string
	cmd    string
	pat    string
	quiet  bool
	failed bool

	reg *regexp.Regexp
}

func NewConfig() *Config {
	return &Config{}
}

func (c *Config) SetRegexp() {
	reg, err := regexp.Compile(c.pat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid regex pattern: %v\n", err)
		os.Exit(1)
	}

	c.reg = reg
}

func (c *Config) GetArgs(host string) []string {
	args := []string{}
	if c.args != "" {
		args = strings.Fields(c.args)
	}

	if c.user != "" {
		if c.path != "" {
			args = append(args, fmt.Sprintf("%s@%s:%s", c.user, host, c.path))
		} else {
			args = append(args, fmt.Sprintf("%s@%s", c.user, host))
		}
	} else {
		args = append(args, host)
	}

	if c.cmd != "" {
		args = append(args, c.cmd)
	}

	return args
}

func (c *Config) PrintCommand(hosts []string) {
	if c.bin == "ssh" {
		fmt.Printf("%s%s[Running] hosts%s: %s\n", blue, highlight, colorOff, strings.Join(hosts, " "))
		fmt.Printf("%s%s[Running] command%s: %s\n", blue, highlight, colorOff, c.cmd)
	} else {
		fmt.Printf("%s%s[Running] hosts%s: %s\n", blue, highlight, colorOff, strings.Join(hosts, " "))
		fmt.Printf("%s%s[Running] action%s: %s %s\n", blue, highlight, colorOff, c.bin, strings.Join(c.GetArgs("[host]"), " "))
	}
}

type Runner struct {
	config    *Config
	host      string
	color     int
	mu        *sync.Mutex
	unmatched []string
	localMu   sync.Mutex
	wg        sync.WaitGroup
}

func NewRunner(config *Config, host string, color int, mu *sync.Mutex) *Runner {
	return &Runner{
		config:    config,
		host:      host,
		color:     color,
		mu:        mu,
		unmatched: []string{},
	}
}

func (r *Runner) Run() Result {
	cmd := exec.Command(r.config.bin, r.config.GetArgs(r.host)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return r.fail(err, "Failed to get stdout pipe")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return r.fail(err, "Failed to get stderr pipe")
	}
	if err := cmd.Start(); err != nil {
		return r.fail(err, "Failed to start command")
	}

	process := func(reader *bufio.Reader) {
		defer r.wg.Done()
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(line, "\n")
				show := r.config.reg.MatchString(line)
				if show {
					r.mu.Lock()
					fmt.Printf("%s: %s\n", colorize(r.color, r.host), line)
					r.mu.Unlock()
				} else {
					r.localMu.Lock()
					r.unmatched = append(r.unmatched, line)
					r.localMu.Unlock()
				}
			}
			if err != nil {
				break
			}
		}
	}

	r.wg.Add(2)
	go process(bufio.NewReader(stdout))
	go process(bufio.NewReader(stderr))
	r.wg.Wait()

	code := 0
	if err = cmd.Wait(); err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}

	r.print(code)
	return Result{
		host: r.host,
		code: code,
	}
}

func (r *Runner) fail(err error, msg string) Result {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintf(os.Stderr, "[%s] %s: %v\n", r.host, msg, err)

	return Result{
		host: r.host,
		code: 1,
	}
}

func (r *Runner) print(code int) {
	if len(r.unmatched) == 0 {
		return
	}

	var lines []string
	if !r.config.quiet {
		lines = r.unmatched
	} else if code > 0 {
		lines = tail(r.unmatched, 15)
	} else {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Printf(">>>>>>>>>>>>>>>> [Output] unmatched(%s):\n", colorize(r.color, r.host))
	var lastLine string
	for _, l := range lines {
		fmt.Println(l)
		lastLine = l
	}
	lastLine = strings.TrimSpace(lastLine)
	if lastLine != "" {
		fmt.Println()
	}
}

func main() {
	config := NewConfig()
	flag.StringVar(&config.bin, "b", "ssh", "Run binary on local machine(1)")
	flag.StringVar(&config.args, "a", "", "Run binary arguments(2)")
	flag.StringVar(&config.user, "u", "", "Login username, user@host(3)")
	flag.StringVar(&config.path, "path", "", "file or directory, user@host:path(4)")
	flag.StringVar(&config.cmd, "c", "", "Run command on remote hosts(5)")

	flag.IntVar(&config.jobs, "j", 1, "Maximum number of concurrent runs")
	flag.StringVar(&config.pat, "p", `.*`, "Regex filter for real-time line display")
	flag.BoolVar(&config.quiet, "q", false, "Only print matched lines")
	flag.Parse()

	hosts := flag.Args()
	if len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] host1 host2 ...\n", os.Args[0])
		flag.Usage()
		os.Exit(1)
	}

	config.SetRegexp()
	config.PrintCommand(hosts)

	sem := make(chan struct{}, config.jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]Result, len(hosts))

	for idx, host := range hosts {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			r := NewRunner(config, h, i, &mu)
			results[i] = r.Run()
		}(idx, host)
	}
	wg.Wait()

	var hasError bool
	var sb strings.Builder
	sb.Grow(len(hosts) * 128)
	for _, res := range results {
		if res.code != 0 {
			hasError = true
			sb.WriteString(res.host)
			sb.WriteString(" ")
		}
	}

	if hasError {
		fmt.Printf("%s[ERROR]%s hosts:\n", red, colorOff)
		fmt.Println(strings.TrimSuffix(sb.String(), " "))
		os.Exit(1)
	}
}
