package common

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-collections/collections/stack"
	versionUtil "github.com/hashicorp/go-version"
	packer "github.com/hashicorp/packer/common"
)

type VBox42Driver struct {
	// This is the path to the "VBoxManage" application.
	VBoxManagePath string
}

func (d *VBox42Driver) CreateSATAController(vmName string, name string, portcount int) error {
	version, err := d.Version()
	if err != nil {
		return err
	}

	portCountArg := "--portcount"

	currentVersion, err := versionUtil.NewVersion(version)
	if err != nil {
		return err
	}
	firstVersionUsingPortCount, err := versionUtil.NewVersion("4.3")
	if err != nil {
		return err
	}

	if currentVersion.LessThan(firstVersionUsingPortCount) {
		portCountArg = "--sataportcount"
	}

	command := []string{
		"storagectl", vmName,
		"--name", name,
		"--add", "sata",
		portCountArg, strconv.Itoa(portcount),
	}

	return d.VBoxManage(command...)
}

func (d *VBox42Driver) CreateSCSIController(vmName string, name string) error {

	command := []string{
		"storagectl", vmName,
		"--name", name,
		"--add", "scsi",
		"--controller", "LSILogic",
	}

	return d.VBoxManage(command...)
}

func (d *VBox42Driver) Delete(name string) error {
	ctx := context.TODO()
	return retry.Config{
		Tries:      5,
		RetryDelay: (&retry.Backoff{InitialBackoff: 1 * time.Second, MaxBackoff: 1 * time.Second, Multiplier: 2}).Linear,
	}.Run(ctx, func(ctx context.Context) error {
		err := d.VBoxManage("unregistervm", name, "--delete")
		return err
	})
}

func (d *VBox42Driver) Iso() (string, error) {
	var stdout bytes.Buffer

	cmd := exec.Command(d.VBoxManagePath, "list", "systemproperties")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}

	DefaultGuestAdditionsRe := regexp.MustCompile("Default Guest Additions ISO:(.+)")

	for _, line := range strings.Split(stdout.String(), "\n") {
		// Need to trim off CR character when running in windows
		// Trimming whitespaces at this point helps to filter out empty value
		line = strings.TrimRight(line, " \r")

		matches := DefaultGuestAdditionsRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		isoname := strings.Trim(matches[1], " \r\n")
		log.Printf("Found Default Guest Additions ISO: %s", isoname)

		return isoname, nil
	}

	return "", fmt.Errorf("Cannot find \"Default Guest Additions ISO\" in vboxmanage output (or it is empty)")
}

func (d *VBox42Driver) Import(name string, path string, flags []string) error {
	args := []string{
		"import", path,
		"--vsys", "0",
		"--vmname", name,
	}
	args = append(args, flags...)

	return d.VBoxManage(args...)
}

func (d *VBox42Driver) IsRunning(name string) (bool, error) {
	var stdout bytes.Buffer

	cmd := exec.Command(d.VBoxManagePath, "showvminfo", name, "--machinereadable")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, err
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		// Need to trim off CR character when running in windows
		line = strings.TrimRight(line, "\r")

		if line == `VMState="running"` {
			return true, nil
		}

		// We consider "stopping" to still be running. We wait for it to
		// be completely stopped or some other state.
		if line == `VMState="stopping"` {
			return true, nil
		}

		// We consider "paused" to still be running. We wait for it to
		// be completely stopped or some other state.
		if line == `VMState="paused"` {
			return true, nil
		}
	}

	return false, nil
}

func (d *VBox42Driver) Stop(name string) error {
	if err := d.VBoxManage("controlvm", name, "poweroff"); err != nil {
		return err
	}

	// We sleep here for a little bit to let the session "unlock"
	time.Sleep(2 * time.Second)

	return nil
}

func (d *VBox42Driver) SuppressMessages() error {
	extraData := map[string]string{
		"GUI/RegistrationData": "triesLeft=0",
		"GUI/SuppressMessages": "confirmInputCapture,remindAboutAutoCapture,remindAboutMouseIntegrationOff,remindAboutMouseIntegrationOn,remindAboutWrongColorDepth",
		"GUI/UpdateDate":       fmt.Sprintf("1 d, %d-01-01, stable", time.Now().Year()+1),
		"GUI/UpdateCheckCount": "60",
	}

	for k, v := range extraData {
		if err := d.VBoxManage("setextradata", "global", k, v); err != nil {
			return err
		}
	}

	return nil
}

func (d *VBox42Driver) VBoxManage(args ...string) error {
	_, err := d.VBoxManageWithOutput(args...)
	return err
}

func (d *VBox42Driver) VBoxManageWithOutput(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	log.Printf("Executing VBoxManage: %#v", args)
	cmd := exec.Command(d.VBoxManagePath, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	if _, ok := err.(*exec.ExitError); ok {
		err = fmt.Errorf("VBoxManage error: %s", stderrString)
	}

	if err == nil {
		// Sometimes VBoxManage gives us an error with a zero exit code,
		// so we also regexp match an error string.
		m, _ := regexp.MatchString("VBoxManage([.a-z]+?): error:", stderrString)
		if m {
			err = fmt.Errorf("VBoxManage error: %s", stderrString)
		}
	}

	log.Printf("stdout: %s", stdoutString)
	log.Printf("stderr: %s", stderrString)

	return stdoutString, err
}

func (d *VBox42Driver) Verify() error {
	return nil
}

func (d *VBox42Driver) Version() (string, error) {
	var stdout bytes.Buffer

	cmd := exec.Command(d.VBoxManagePath, "--version")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}

	versionOutput := strings.TrimSpace(stdout.String())
	log.Printf("VBoxManage --version output: %s", versionOutput)

	// If the "--version" output contains vboxdrv, then this is indicative
	// of problems with the VirtualBox setup and we shouldn't really continue,
	// whether or not we can read the version.
	if strings.Contains(versionOutput, "vboxdrv") {
		return "", fmt.Errorf("VirtualBox is not properly setup: %s", versionOutput)
	}

	versionRe := regexp.MustCompile("^([.0-9]+)(?:_(?:RC|OSEr)[0-9]+)?")
	matches := versionRe.FindAllStringSubmatch(versionOutput, 1)
	if matches == nil || len(matches[0]) != 2 {
		return "", fmt.Errorf("No version found: %s", versionOutput)
	}

	log.Printf("VirtualBox version: %s", matches[0][1])
	return matches[0][1], nil
}

// LoadSnapshots load the snapshots for a VM instance
func (d *VBox42Driver) LoadSnapshots(vmName string) (*VBoxSnapshot, error) {
	if vmName == "" {
		panic("Argument empty exception: vmName")
	}
	log.Printf("Executing LoadSnapshots: VM: %s", vmName)

	stdoutString, err := d.VBoxManageWithOutput("snapshot", vmName, "list", "--machinereadable")
	if nil != err {
		return nil, err
	}

	var rootNode *VBoxSnapshot
	if stdoutString != "This machine does not have any snapshots" {
		scanner := bufio.NewScanner(strings.NewReader(stdoutString))
		SnapshotNamePartsRe := regexp.MustCompile("Snapshot(?P<Type>Name|UUID)(?P<Path>(-[1-9]+)*)=\"(?P<Value>[^\"]*)\"")
		var currentIndicator string
		parentStack := stack.New()
		var node *VBoxSnapshot
		for scanner.Scan() {
			txt := scanner.Text()
			idx := strings.Index(txt, "=")
			if idx > 0 {
				if strings.HasPrefix(txt, "Current") {
					node.IsCurrent = true
				} else {
					matches := SnapshotNamePartsRe.FindStringSubmatch(txt)
					log.Printf("************ Snapshot %s name parts", txt)
					log.Printf("Matches %#v\n", matches)
					log.Printf("Node %s\n", matches[0])
					log.Printf("Type %s\n", matches[1])
					log.Printf("Path %s\n", matches[2])
					log.Printf("Leaf %s\n", matches[3])
					log.Printf("Value %s\n", matches[4])
					if matches[1] == "Name" {
						if nil == rootNode {
							node = new(VBoxSnapshot)
							rootNode = node
							currentIndicator = matches[2]
						} else {
							pathLenCur := strings.Count(currentIndicator, "-")
							pathLen := strings.Count(matches[2], "-")
							if pathLen > pathLenCur {
								currentIndicator = matches[2]
								parentStack.Push(node)
							} else if pathLen < pathLenCur {
								for i := 0; i < pathLenCur-1; i++ {
									parentStack.Pop()
								}
							}
							node = new(VBoxSnapshot)
							parent := parentStack.Peek().(*VBoxSnapshot)
							if nil != parent {
								parent.Children = append(parent.Children, node)
							}
						}
						node.Name = matches[4]
					} else if matches[1] == "UUID" {
						node.UUID = matches[4]
					}
				}
			} else {
				log.Printf("Invalid key,value pair [%s]", txt)
			}
		}
	}

	return rootNode, nil
}

func (d *VBox42Driver) CreateSnapshot(vmname string, snapshotName string) error {
	if vmname == "" {
		panic("Argument empty exception: vmname")
	}
	log.Printf("Executing CreateSnapshot: VM: %s, SnapshotName %s", vmname, snapshotName)

	return d.VBoxManage("snapshot", vmname, "take", snapshotName)
}

func (d *VBox42Driver) HasSnapshots(vmname string) (bool, error) {
	if vmname == "" {
		panic("Argument empty exception: vmname")
	}
	log.Printf("Executing HasSnapshots: VM: %s", vmname)

	sn, err := d.LoadSnapshots(vmname)
	if nil != err {
		return false, err
	}
	return nil != sn, nil
}

func (d *VBox42Driver) GetCurrentSnapshot(vmname string) (*VBoxSnapshot, error) {
	if vmname == "" {
		panic("Argument empty exception: vmname")
	}
	log.Printf("Executing GetCurrentSnapshot: VM: %s", vmname)

	sn, err := d.LoadSnapshots(vmname)
	if nil != err {
		return nil, err
	}
	return sn.GetCurrentSnapshot(), nil
}

func (d *VBox42Driver) SetSnapshot(vmname string, sn *VBoxSnapshot) error {
	if vmname == "" {
		panic("Argument empty exception: vmname")
	}
	if nil == sn {
		panic("Argument null exception: sn")
	}
	log.Printf("Executing SetSnapshot: VM: %s, SnapshotName %s", vmname, sn.UUID)

	return d.VBoxManage("snapshot", vmname, "restore", sn.UUID)
}

func (d *VBox42Driver) DeleteSnapshot(vmname string, sn *VBoxSnapshot) error {
	if vmname == "" {
		panic("Argument empty exception: vmname")
	}
	if nil == sn {
		panic("Argument null exception: sn")
	}
	log.Printf("Executing DeleteSnapshot: VM: %s, SnapshotName %s", vmname, sn.UUID)
	return d.VBoxManage("snapshot", vmname, "delete", sn.UUID)
}

/*
func (d *VBox42Driver) SnapshotExists(vmname string, snapshotName string) (bool, error) {
	log.Printf("Executing SnapshotExists: VM %s, SnapshotName %s", vmname, snapshotName)

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(d.VBoxManagePath, "snapshot", vmname, "list", "--machinereadable")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if _, ok := err.(*exec.ExitError); ok {
		stderrString := strings.TrimSpace(stderr.String())
		return false, (fmt.Errorf("VBoxManage error: %s", stderrString))
	}

	SnapshotNameRe := regexp.MustCompile(fmt.Sprintf("SnapshotName[^=]*=[^\"]*\"%s\"", snapshotName))

	for _, line := range strings.Split(stdout.String(), "\n") {
		if SnapshotNameRe.MatchString(line) {
			return true, nil
		}
	}

	return false, nil
}

func (d *VBox42Driver) GetParentSnapshot(vmname string, snapshotName string) (string, error) {
	log.Printf("Executing GetParentSnapshot: VM %s, SnapshotName %s", vmname, snapshotName)

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(d.VBoxManagePath, "snapshot", vmname, "list", "--machinereadable")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if _, ok := err.(*exec.ExitError); ok {
		stderrString := strings.TrimSpace(stderr.String())
		return "", (fmt.Errorf("VBoxManage error: %s", stderrString))
	}

	SnapshotNameRe := regexp.MustCompile(fmt.Sprintf("SnapshotName[^=]*=[^\"]*\"%s\"", snapshotName))

	var snapshot string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if SnapshotNameRe.MatchString(line) {
			snapshot = line
			break
		}
	}

	if snapshot == "" {
		return "", (fmt.Errorf("Snapshot %s does not exist", snapshotName))
	}

	SnapshotNamePartsRe := regexp.MustCompile("SnapshotName(?P<Path>(-[1-9]+)*)")
	matches := SnapshotNamePartsRe.FindStringSubmatch(snapshot)
	log.Printf("************ Snapshot %s name parts", snapshot)
	log.Printf("Matches %#v\n", matches)
	log.Printf("Node %s\n", matches[0])
	log.Printf("Path %s\n", matches[1])
	log.Printf("Leaf %s\n", matches[2])
	leaf := matches[2]
	node := matches[0]
	if node == "" {
		return "", (fmt.Errorf("Unsupported format for snapshot %s", snapshot))
	}
	if leaf != "" && node != "" {
		SnapshotNodeRe := regexp.MustCompile("^(?P<Node>SnapshotName[^=]*)=[^\"]*\"(?P<Name>[^\"]+)\"")
		parentNode := node[:len(node)-len(leaf)]
		log.Printf("Parent node %s\n", parentNode)
		var parentName string
		for _, line := range strings.Split(stdout.String(), "\n") {
			if matches := SnapshotNodeRe.FindStringSubmatch(line); len(matches) > 1 && parentNode == matches[1] {
				parentName = matches[2]
				log.Printf("Parent Snapshot name %s\n", parentName)
				break
			}
		}
		if parentName == "" {
			return "", (fmt.Errorf("Internal error: Unable to find name for snapshot node %s", parentNode))
		}
		return parentName, nil
	}
	return "", nil
}
*/
