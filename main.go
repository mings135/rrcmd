package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// RunCommand struct to hold command information
type RunCommand struct {
	host    string
	command string
	lock    *sync.Mutex
	result  int
}

// Run executes the command and prints the output
func (rc *RunCommand) Run(wg *sync.WaitGroup) {
	defer wg.Done()

	// Execute the command
	cmd := exec.Command("ssh", rc.host, rc.command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		// fmt.Printf("Error getting stdout pipe: %v\n", err)
		rc.result = 1
		return
	}
	if err := cmd.Start(); err != nil {
		// fmt.Printf("Error starting command: %v\n", err)
		rc.result = 1
		return
	}

	// Read and print the command output
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		rc.lock.Lock()
		fmt.Println(scanner.Text())
		rc.lock.Unlock()
	}

	if err := scanner.Err(); err != nil {
		// fmt.Printf("Error reading stdout: %v\n", err)
		rc.result = 1
		return
	}

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
		fmt.Println("Usage: <username> <command> <ip> ...")
		os.Exit(1)
	}

	username := os.Args[1]
	command := os.Args[2]
	addrs := os.Args[3:]

	var wg sync.WaitGroup
	var mu sync.Mutex
	var resultSum int
	rcs := make([]*RunCommand, 0, len(addrs))

	for _, addr := range addrs {
		host := fmt.Sprintf("%s@%s", username, addr)
		rc := &RunCommand{host: host, command: command, lock: &mu}
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
