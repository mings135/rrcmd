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
	ColorOff  = "\033[0m"
	Red       = "\033[31m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Purple    = "\033[35m"
	Cyan      = "\033[36m"
	HighLight = "\033[1m"
)

var colors = []string{Red, Green, Yellow, Blue, Purple, Cyan}

// Colorize returns input with color code.
func Colorize(colorID int, input string) string {
	c := colors[colorID%len(colors)]
	return fmt.Sprintf("%s%s%s", c, input, ColorOff)
}

type Result struct {
	Host      string
	Err       error
	ExitCode  int
	Unmatched []string
}

func main() {
	// 参数解析
	var (
		cmdStr  string
		user    string
		jobs    int
		sshArgs string
		pattern string
		quiet   bool
	)
	flag.StringVar(&cmdStr, "c", "echo ok", "Command to run")
	flag.StringVar(&user, "u", "", "Username for ssh login")
	flag.IntVar(&jobs, "j", 1, "Max concurrency for ssh execution")
	flag.StringVar(&sshArgs, "a", "-o BatchMode=yes", "Custom ssh arguments")
	flag.StringVar(&pattern, "p", `^\[.*\].* \[.*\]$`, `Regex to match output lines to print real time`)
	flag.BoolVar(&quiet, "q", false, "Quiet mode, only print matched lines")
	flag.Parse()

	hosts := flag.Args()
	if len(cmdStr) == 0 || len(user) == 0 || len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s -u <user> -c <command> [options] host1 host2 ...\n", os.Args[0])
		flag.Usage()
		os.Exit(1)
	}

	pat, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid regex pattern: %v\n", err)
		os.Exit(1)
	}

	sem := make(chan struct{}, jobs) // 信号量，控制并发数量
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]Result, len(hosts))

	fmt.Printf("%s%sCommand%s: %s\n", Blue, HighLight, ColorOff, cmdStr)
	fmt.Printf("%s%sHosts%s: %v\n", Blue, HighLight, ColorOff, hosts)

	for idx, host := range hosts {
		wg.Add(1)
		go func(i int, h string, colorID int) {
			defer wg.Done()
			sem <- struct{}{}
			res := runForHost(user, h, cmdStr, sshArgs, pat, colorID, quiet, &mu)
			results[i] = res
			<-sem
		}(idx, host, idx)
	}

	wg.Wait()

	// 汇报错误
	var hasError bool
	for _, res := range results {
		if res.Err != nil || res.ExitCode != 0 {
			hasError = true
			fmt.Printf("%s[ERROR]%s Host: %s, %v\n",
				Red, ColorOff, res.Host, res.Err)
		}
	}

	if hasError {
		os.Exit(1)
	}
}

func runForHost(user, host, command, sshArgs string, pat *regexp.Regexp, colorID int, quiet bool, mu *sync.Mutex) Result {
	// 构造 ssh 命令
	sshBin := "ssh"
	sshFields := []string{}
	if sshArgs != "" {
		sshFields = strings.Fields(sshArgs)
	}
	sshFields = append(sshFields, fmt.Sprintf("%s@%s", user, host), command)
	cmd := exec.Command(sshBin, sshFields...)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var unmatched []string
	startErr := cmd.Start()
	if startErr != nil {
		return Result{Host: host, Err: fmt.Errorf("Start failed: %v", startErr), ExitCode: 1}
	}

	// 实时分流输出
	var wg sync.WaitGroup
	process := func(reader *bufio.Reader) {
		defer wg.Done()
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				show := pat.MatchString(strings.TrimRight(line, "\n"))
				mu.Lock()
				if show {
					fmt.Printf("%s: %s", Colorize(colorID, host), line)
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
		// 获取退出码
		if exiterr, ok := err.(*exec.ExitError); ok {
			exitCode = exiterr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	mu.Lock()
	if len(unmatched) > 0 && !quiet {
		fmt.Printf(">>>>>>>>>>>>>>>> %s Unmatched output:\n", Colorize(colorID, host))
		for _, l := range unmatched {
			fmt.Print(l)
		}
		fmt.Println()
	}

	mu.Unlock()

	return Result{
		Host:      host,
		Err:       err,
		ExitCode:  exitCode,
		Unmatched: unmatched,
	}
}
