package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Color constants
const (
	Red       = "\033[31m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Purple    = "\033[35m"
	Cyan      = "\033[36m"
	ColorOff  = "\033[0m"
	HighLight = "\033[1m"
)

// Colorize function to return colored string
func Colorize(colorID int, input string) string {
	var color string
	switch colorID {
	case 1:
		color = Red
	case 2:
		color = Green
	case 3:
		color = Yellow
	case 4:
		color = Blue
	case 5:
		color = Purple
	case 6:
		color = Cyan
	default:
		return input
	}
	return color + input + ColorOff
}

// RunCommand struct to hold command information
type RunCommand struct {
	username string
	host     string
	command  string
	lock     *sync.Mutex
	color    int
	result   int
}

// Run executes the command and prints the output
func (rc *RunCommand) Run(wg *sync.WaitGroup) {
	defer wg.Done()

	// Execute the command
	cmd := exec.Command("ssh", fmt.Sprintf("%s@%s", rc.username, rc.host), rc.command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		// fmt.Printf("Error getting stdout pipe: %v\n", err)
		rc.result = 1
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		// fmt.Printf("Error getting stderr pipe: %v\n", err)
		rc.result = 1
		return
	}
	if err := cmd.Start(); err != nil {
		// fmt.Printf("Error starting command: %v\n", err)
		rc.result = 1
		return
	}

	// Read and print the command stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			rc.lock.Lock()
			fmt.Printf("%v: %v\n", Colorize(rc.color, rc.host), scanner.Text())
			rc.lock.Unlock()
		}
		if err := scanner.Err(); err != nil {
			// fmt.Printf("Error reading stdout: %v\n", err)
			rc.result = 1
			return
		}
	}()

	// Read and print the command stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			rc.lock.Lock()
			fmt.Printf("%v: %v\n", Colorize(rc.color, rc.host), scanner.Text())
			rc.lock.Unlock()
		}
		if err := scanner.Err(); err != nil {
			// fmt.Printf("Error reading stderr: %v\n", err)
			rc.result = 1
			return
		}
	}()

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		// fmt.Printf("Error waiting for command: %v\n", err)
		rc.result = 1
		return
	}

	rc.result = 0
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: <username> <command> <host> ...")
		os.Exit(1)
	}

	username := os.Args[1]
	command := os.Args[2]
	hosts := os.Args[3:]
	fmt.Printf("%v%vRun command%v: %v\n", Blue, HighLight, ColorOff, command)
	fmt.Printf("%v%vRemote host%v: %v\n", Blue, HighLight, ColorOff, hosts)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var resultSum int
	var color int
	rcs := make([]*RunCommand, 0, len(hosts))

	for key, host := range hosts {
		color = key % 7
		rc := &RunCommand{
			username: username,
			host:     host,
			command:  command,
			lock:     &mu,
			color:    color,
		}
		rcs = append(rcs, rc)
		wg.Add(1)
		go rc.Run(&wg)
	}

	wg.Wait()
	for _, rc := range rcs {
		resultSum += rc.result
	}

	if resultSum > 0 {
		os.Exit(1)
	}
}
