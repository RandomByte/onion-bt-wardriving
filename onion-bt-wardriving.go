package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/peterbourgon/diskv"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type device struct {
	Name     string
	Count    int
	LastSeen int64
}

var re = regexp.MustCompile("(?im)^[^0-9a-f]*((?:[0-9a-f]{2}:){5}[0-9a-f]{2})\\s*([^\\s].*)?$")
var buffer = make([]string, 8, 8)

var dv *diskv.Diskv

func main() {
	setupPersistence()
	setupBt()
	setupOled()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer signal.Stop(sig)

	for {
		select {
		case <-time.After(1 * time.Millisecond):
			loop()
		case s := <-sig:
			fmt.Println("Got signal:", s)
			fmt.Println("Quitting...")
			return
		}
	}
}

func loop() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered", r)
		}
	}()
	result := scan()
	parsed := parse(result)
	var somethingHappend bool

	for mac, device := range parsed {
		knownDevice := readDevice(mac)
		if knownDevice == nil {
			handleNewDevice(mac, device)
			somethingHappend = true
		} else {
			ignored := handleKnownDevice(mac, device, *knownDevice)
			if ignored != true {
				somethingHappend = true
			}
		}
	}

	if somethingHappend == true {
		// Something happened
		flushOled()
		notify()
	}
}

func handleNewDevice(mac string, device device) {
	fmt.Printf("New device %s: %s\n", device.Name, mac)
	writeOled(device)
	persist(mac, device)
}

func handleKnownDevice(mac string, device device, knownDevice device) bool {
	if time.Since(time.Unix(knownDevice.LastSeen, 0)).Hours() < 5 {
		// Last seen less then five hours ago
		return true
	}

	if device.Name != knownDevice.Name {
		fmt.Printf("Same MAC but different name: %s (new) vs. %s (known)\n", device.Name, knownDevice.Name)

		err := dv.Write("nameclash"+mac+string(time.Now().Unix()), []byte(fmt.Sprintf("%s, %s (new) vs. %s (known)", mac, device.Name, knownDevice.Name)))
		if err != nil {
			fmt.Println(err)
		}
	}

	device.Count = knownDevice.Count + 1
	fmt.Printf("%vx Known device %s: %s\n", device.Count, device.Name, mac)
	writeOled(device)
	persist(mac, device)

	return false
}

func scan() string {
	// Create an *exec.Cmd
	cmd := exec.Command("hcitool", "scan", "--flush")

	// Stdout buffer
	cmdOutput := &bytes.Buffer{}
	// Attach buffer to command
	cmd.Stdout = cmdOutput

	err := cmd.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	return cmdOutput.String()
}

func parse(rawScanResult string) map[string]device {
	matches := re.FindAllStringSubmatch(rawScanResult, -1)
	devices := make(map[string]device)
	for _, match := range matches {
		name := match[2]
		if name == "" {
			name = match[1]
		}
		devices[match[1]] = device{Name: name, LastSeen: time.Now().Unix()}
	}

	return devices
}

func readDevice(mac string) *device {
	value, err := dv.Read(mac)
	if err != nil {
		return nil
	}

	res := &device{}
	json.Unmarshal([]byte(value), res)

	return res
}

func setupPersistence() {
	// Simplest transform function: put all the data files into the base dir.
	flatTransform := func(s string) []string { return []string{} }

	// Initialize a new diskv store, rooted at "diskv-data", with a 1MB cache.
	dv = diskv.New(diskv.Options{
		BasePath:     "diskv-data",
		Transform:    flatTransform,
		CacheSizeMax: 1024 * 1024,
	})
}

func persist(mac string, device device) {
	serialized, _ := json.Marshal(device)
	err := dv.Write(mac, []byte(serialized))
	if err != nil {
		panic(err)
	}
}

func setupBt() {
	cmd := exec.Command("hciconfig", "hci0", "up")

	err := cmd.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
	}
}

func setupOled() {
	cmd := exec.Command("oled-exp", "-i")

	err := cmd.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
	}
}

func writeOled(device device) {
	msg := fmt.Sprintf("%s (%vx)", device.Name, device.Count)

	_, buffer = buffer[len(buffer)-1], buffer[:len(buffer)-1]
	buffer = append([]string{msg}, buffer...)
}

func getOledMsg() string {
	return strings.Join(buffer, "\n")
}

func flushOled() {
	cmd := exec.Command("/bin/sh", "write-oled.sh", "\""+getOledMsg()+"\"")

	// cmd := exec.Command("/usr/sbin/oled-exp", "cursor 0,0 write stf")
	fmt.Printf("==> Executing: %s\n", strings.Join(cmd.Args, " "))

	// Stdout buffer
	cmdOutput := &bytes.Buffer{}
	// Attach buffer to command
	cmd.Stdout = cmdOutput

	err := cmd.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("==> Output: %s\n", string(cmdOutput.Bytes()))
}

func notify() {
	cmdBlue := exec.Command("expled", "0x0000ff")

	err := cmdBlue.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
	}

	cmdOff := exec.Command("expled", "0x000000")
	err = cmdOff.Run() // will wait for command to return
	if err != nil {
		fmt.Println(err)
	}
}