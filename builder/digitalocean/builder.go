// The digitalocean package contains a packer.Builder implementation
// that builds DigitalOcean images (snapshots).

package digitalocean

import (
	"log"
	"time"

	"github.com/digitalocean/godo"
	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/common"
	"github.com/mitchellh/packer/packer"
	"golang.org/x/oauth2"
)

// see https://api.digitalocean.com/images/?client_id=[client_id]&api_key=[api_key]
// name="Ubuntu 12.04.4 x64", id=6374128,
const DefaultImage = "ubuntu-12-04-x64"

// see https://api.digitalocean.com/regions/?client_id=[client_id]&api_key=[api_key]
// name="New York 3", id=8
const DefaultRegion = "nyc3"

// see https://api.digitalocean.com/sizes/?client_id=[client_id]&api_key=[api_key]
// name="512MB", id=66 (the smallest droplet size)
const DefaultSize = "512mb"

// The unique id for the builder
const BuilderId = "pearkes.digitalocean"

type Builder struct {
	config Config
	runner multistep.Runner
}

func (b *Builder) Prepare(raws ...interface{}) ([]string, error) {
	c, warnings, errs := NewConfig(raws...)
	if errs != nil {
		return warnings, errs
	}
	b.config = *c

	return nil, nil
}

func (b *Builder) Run(ui packer.Ui, hook packer.Hook, cache packer.Cache) (packer.Artifact, error) {
	client := godo.NewClient(oauth2.NewClient(oauth2.NoContext, &apiTokenSource{
		AccessToken: b.config.APIToken,
	}))

	// Set up the state
	state := new(multistep.BasicStateBag)
	state.Put("config", b.config)
	state.Put("client", client)
	state.Put("hook", hook)
	state.Put("ui", ui)

	// Build the steps
	steps := []multistep.Step{
		new(stepCreateSSHKey),
		new(stepCreateDroplet),
		new(stepDropletInfo),
		&common.StepConnectSSH{
			SSHAddress:     sshAddress,
			SSHConfig:      sshConfig,
			SSHWaitTimeout: 5 * time.Minute,
		},
		new(common.StepProvision),
		new(stepShutdown),
		new(stepPowerOff),
		new(stepSnapshot),
	}

	// Run the steps
	if b.config.PackerDebug {
		b.runner = &multistep.DebugRunner{
			Steps:   steps,
			PauseFn: common.MultistepDebugFn(ui),
		}
	} else {
		b.runner = &multistep.BasicRunner{Steps: steps}
	}

	b.runner.Run(state)

	// If there was an error, return that
	if rawErr, ok := state.GetOk("error"); ok {
		return nil, rawErr.(error)
	}

	if _, ok := state.GetOk("snapshot_name"); !ok {
		log.Println("Failed to find snapshot_name in state. Bug?")
		return nil, nil
	}

	artifact := &Artifact{
		snapshotName: state.Get("snapshot_name").(string),
		snapshotId:   state.Get("snapshot_image_id").(int),
		regionName:   state.Get("region").(string),
		client:       client,
	}

	return artifact, nil
}

func (b *Builder) Cancel() {
	if b.runner != nil {
		log.Println("Cancelling the step runner...")
		b.runner.Cancel()
	}
}
