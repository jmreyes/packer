package proxmox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

// stepUploadISO uploads an ISO file to Proxmox so we can boot from it
type stepUploadISO struct {
	ShouldUpload         bool
	DownloadPathKey      string
	TargetStoragePathKey string
	ISOUrls              []string
	ISOFile              string
}

type uploader interface {
	Upload(node string, storage string, contentType string, filename string, file io.Reader) error
}

var _ uploader = &proxmox.Client{}

func (s *stepUploadISO) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	client := state.Get("proxmoxClient").(uploader)
	c := state.Get("config").(*Config)

	if !s.ShouldUpload {
		state.Put(s.TargetStoragePathKey, s.ISOFile)
		return multistep.ActionContinue
	}

	p := state.Get(s.DownloadPathKey).(string)
	if p == "" {
		err := fmt.Errorf("Path to downloaded ISO was empty")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// All failure cases in resolving the symlink are caught anyway in os.Open
	isoPath, _ := filepath.EvalSymlinks(p)
	r, err := os.Open(isoPath)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	filename := filepath.Base(s.ISOUrls[0])
	ui.Say(fmt.Sprintf("Uploading %s...", filename))
	err = client.Upload(c.Node, c.ISOStoragePool, "iso", filename, r)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	isoStoragePath := fmt.Sprintf("%s:iso/%s", c.ISOStoragePool, filename)
	state.Put(s.TargetStoragePathKey, isoStoragePath)

	return multistep.ActionContinue
}

func (s *stepUploadISO) Cleanup(state multistep.StateBag) {
}
