// Copyright (c) 2018 Huawei Technologies Co., Ltd. All Rights Reserved.
//
//    Licensed under the Apache License, Version 2.0 (the "License"); you may
//    not use this file except in compliance with the License. You may obtain
//    a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
//    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
//    License for the specific language governing permissions and limitations
//    under the License.

package nfs

import (
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/opensds/opensds/pkg/utils/exec"
)

type Cli struct {
	// Command executer
	BaseExecuter exec.Executer
	// Command Root executer
	RootExecuter exec.Executer
}

func NewCli() (*Cli, error) {
	return &Cli{
		BaseExecuter: exec.NewBaseExecuter(),
		RootExecuter: exec.NewRootExecuter(),
	}, nil
}

func (c *Cli) execute(cmd ...string) (string, error) {
	// glog.Infof("i m here at execute...", len(cmd))
	return c.RootExecuter.Run(cmd[0], cmd[1:]...)
	//return c.RootExecuter.Run(cmd)
}

func sizeStr(size int64) string {
	return fmt.Sprintf("%dg", size)
}

func (c *Cli) GetExportLocation(share_name, ip string) string {
	server := net.ParseIP(ip)
	if server == nil {
		glog.Errorf("This is not a valid ip:")
		return ""
	}
	export_location := path.Join(string(server), ":", "/var/", share_name)
	return export_location
}

func (c *Cli) Mount(lvPath, dirName string) error {
	cmd := []string{
		"env", "LC_ALL=C",
		"mount",
		lvPath,
		dirName,
	}
	_, err := c.execute(cmd...)
	return err
}

func (c *Cli) CreateDirectory(dirName string) error {
	cmd := []string{
		"env", "LC_ALL=C",
		"mkdir",
		dirName,
	}
	_, err := c.execute(cmd...)
	return err
}

func (c *Cli) CreateFileShare(lvPath string) error {
	// create a filesytem
	cmd := []string{
		"env", "LC_ALL=C",
		"mke2fs",
		lvPath,
	}
	out := cmd
	glog.Infof(": CMD: %s, RESPONSE: %s", strings.Join(cmd, " "), out)
	_, err := c.execute(cmd...)
	return err
}

func (c *Cli) CreateVolume(name string, vg string, size int64) error {
	cmd := []string{
		"env", "LC_ALL=C",
		"lvcreate",
		"-Z", "n",
		"-n", name,
		"-L", sizeStr(size),
		vg,
	}
	_, err := c.execute(cmd...)
	if err == nil {
		// Deal with the error, probably pushing it up the call stack
		return err
	}

	return err
}

func (c *Cli) Exists(name string) bool {
	cmd := []string{
		"env", "LC_ALL=C",
		"lvs",
		"--noheadings",
		"-o", "name",
	}
	out, err := c.execute(cmd...)
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(out) {
		if field == name {
			return true
		}
	}
	return false
}

func (c *Cli) LvIsActivate(name, vg string) bool {
	cmd := []string{
		"env", "LC_ALL=C",
		"lvdisplay",
		"--noheading",
		"-C", "-o",
		"Attr", path.Join(vg, name),
	}
	out, err := c.execute(cmd...)
	if err != nil {
		glog.Error("Failed to display logic volume:", err)
		return false
	}
	out = strings.TrimSpace(out)
	return out[4] == 'a'
}

// delete volume or snapshot
func (c *Cli) Delete(name, vg string) error {
	// LV removal seems to be a race with other writers so we enable retry deactivation
	lvmConfig := "activation { retry_deactivation = 1} "
	cmd := []string{
		"env", "LC_ALL=C",
		"lvremove",
		"--config", lvmConfig,
		"-f",
		path.Join(vg, name),
	}

	if out, err := c.execute(cmd...); err != nil {
		glog.Infof("Error reported running lvremove: CMD: %s, RESPONSE: %s",
			strings.Join(cmd, " "), out)
		// run_udevadm_settle
		c.execute("udevadm", "settle")

		// The previous failing lvremove -f might leave behind
		// suspended devices; when lvmetad is not available, any
		// further lvm command will block forever.
		// Therefore we need to skip suspended devices on retry.
		lvmConfig += "devices { ignore_suspended_devices = 1}"
		cmd := []string{
			"env", "LC_ALL=C",
			"lvremove",
			"--config", lvmConfig,
			"-f",
			path.Join(vg, name),
		}
		if _, err := c.execute(cmd...); err != nil {
			return err
		}
		glog.Infof("Successfully deleted volume: %s after udev settle.", name)
	}
	return nil
}

type VolumeGroup struct {
	Name          string
	TotalCapacity int64
	FreeCapacity  int64
	UUID          string
}

func (c *Cli) ListVgs() (*[]VolumeGroup, error) {
	cmd := []string{
		"env", "LC_ALL=C",
		"vgs",
		"--noheadings",
		"--nosuffix",
		"--unit=g",
		"-o", "name,size,free,uuid",
	}
	out, err := c.execute(cmd...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	var vgs []VolumeGroup
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		fields := strings.Fields(line)
		total, _ := strconv.ParseFloat(fields[1], 64)
		free, _ := strconv.ParseFloat(fields[2], 64)
		vg := VolumeGroup{
			Name:          fields[0],
			TotalCapacity: int64(total),
			FreeCapacity:  int64(free),
			UUID:          fields[3],
		}
		vgs = append(vgs, vg)
	}
	return &vgs, nil
}
