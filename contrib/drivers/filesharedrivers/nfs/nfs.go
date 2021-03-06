// Copyright (c) 2017 Huawei Technologies Co., Ltd. All Rights Reserved.
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
	"path"
	"fmt"

	log "github.com/golang/glog"
	. "github.com/opensds/opensds/contrib/drivers/utils/config"
	"github.com/opensds/opensds/pkg/model"
	pb "github.com/opensds/opensds/pkg/model/proto"
	"github.com/opensds/opensds/pkg/utils/config"
	"github.com/satori/go.uuid"
)

const (
	defaultTgtConfDir = "/etc/tgt/conf.d"
	defaultTgtBindIp  = "127.0.0.1"
	defaultConfPath   = "/etc/opensds/driver/nfs.yaml"
	volumePrefix      = "volume-"
	snapshotPrefix    = "_snapshot-"
	blocksize         = 4096
	sizeShiftBit      = 30
	opensdsnvmepool   = "opensds-nvmegroup"
	nvmeofAccess      = "nvmeof"
	iscsiAccess       = "iscsi"
)

const (
	KLvPath        = "lvPath"
	KLvsPath       = "lvsPath"
	KFileshareName = "lvmFileshareName"
	KFileshareID   = "lvmFileshareID"
)

type NFSConfig struct {
	TgtBindIp      string                    `yaml:"tgtBindIp"`
	TgtConfDir     string                    `yaml:"tgtConfDir"`
	EnableChapAuth bool                      `yaml:"enableChapAuth"`
	Pool           map[string]PoolProperties `yaml:"pool,flow"`
}

type Driver struct {
	conf *NFSConfig
	cli  *Cli
}

func (d *Driver) Setup() error {
	// Read lvm config file
	d.conf = &NFSConfig{TgtBindIp: defaultTgtBindIp, TgtConfDir: defaultTgtConfDir}
	//p := config.CONF.OsdsDock.Nfs_Back.NFSNative.ConfigPath
  p := config.CONF.OsdsDock.Backends.NFS.ConfigPath
	if "" == p {
		p = defaultConfPath
	}
	if _, err := Parse(d.conf, p); err != nil {
		return err
	}
	cli, err := NewCli()
	if err != nil {
		return err
	}
	d.cli = cli

	return nil
}

func (*Driver) Unset() error { return nil }

func (d *Driver) CreateFileShare(opt *pb.CreateFileShareOpts) (fshare *model.FileShareSpec, err error) {
	fmt.Println("i im in driver code..")
	fmt.Println("server == ")
	//get the server ip for configuration
	//var server = d.conf.TgtBindIp
	//fmt.Println("server == ",server)

	var name = opt.GetName()
	//var vg = opt.GetPoolName()
	vg:= "opensds-files-default"
	fmt.Println("namee == ",name)
	fmt.Println("vg == ",vg)
	// Crete a directory to mount
	var dirName = path.Join("/var/", name)
	// create a fileshare path
	var lvPath = path.Join("/dev", vg, name)
	if err = d.cli.CreateVolume(name, vg, opt.GetSize()); err != nil {
		return
	}
	// remove created volume if got error
	defer func() {
		// using return value as the error flag
		if fshare == nil {
			if err := d.cli.Delete(name, vg); err != nil {
				log.Error("Failed to remove volume fileshare:", err)
			}
		}
	}()

	if err := d.cli.CreateDirectory(dirName); err != nil {
		log.Error("Failed to create a directory:", err)
		return nil, err
	}

	// Crete fileshare on this path
	if err := d.cli.CreateFileShare(lvPath); err != nil {
		log.Error("Failed to create filesystem logic volume:", err)
		return nil, err
	}

	// mount the volume to directory
	if err := d.cli.Mount(lvPath, dirName); err != nil {
		log.Error("Failed to mount a directory:", err)
		return nil, err
	}

	//location := d.cli.GetExportLocation(name, server)
	//if location == "" {
	//	log.Error("Failed to get Export location:", err)
	//	return nil, err
	//}
	//var export_location = path.Join(ip, ":", dirName)
  location := "0.0.0.0"
	ffshare := &model.FileShareSpec{
		BaseModel: &model.BaseModel{
			Id: opt.GetId(),
		},
		Name:             opt.GetName(),
		Size:             opt.GetSize(),
		Description:      opt.GetDescription(),
		AvailabilityZone: opt.GetAvailabilityZone(),
		PoolId:           vg,
		ExportLocations:  location,
		Metadata: map[string]string{
			KFileshareName: name,
			KFileshareID:   "123",
		},
	}
	return ffshare, nil
}

// ListPools
func (d *Driver) ListPools() ([]*model.StoragePoolSpec, error) {
fmt.Println("i im in pool list code..")
	vgs, err := d.cli.ListVgs()
	if err != nil {
		return nil, err
	}
	var pols []*model.StoragePoolSpec
	for _, vg := range *vgs {
		if _, ok := d.conf.Pool[vg.Name]; !ok {
			continue
		}

		pol := &model.StoragePoolSpec{
			BaseModel: &model.BaseModel{
				Id: uuid.NewV5(uuid.NamespaceOID, vg.UUID).String(),
			},
			Name:             vg.Name,
			TotalCapacity:    vg.TotalCapacity,
			FreeCapacity:     vg.FreeCapacity,
			StorageType:      d.conf.Pool[vg.Name].StorageType,
			Extras:           d.conf.Pool[vg.Name].Extras,
			AvailabilityZone: d.conf.Pool[vg.Name].AvailabilityZone,
		}
		if pol.AvailabilityZone == "" {
			pol.AvailabilityZone = "default"
		}
		pols = append(pols, pol)
	}
	return pols, nil
}

func (d *Driver) DeleteFileShare(opt *pb.DeleteFileShareOpts) (fshare *model.FileShareSpec, err error) {
  /*
	var name = volumePrefix + opt.GetId()
	if !d.cli.Exists(name) {
		log.Warningf("Volume(%s) does not exist, nothing to remove", name)
		return nil
	}

	lvPath, ok := opt.GetMetadata()[KLvPath]
	if !ok {
		err := errors.New("can't find 'lvPath' in volume metadata")
		log.Error(err)
		return err
	}

	field := strings.Split(lvPath, "/")
	vg := field[2]

	if err := d.cli.Delete(name, vg); err != nil {
		log.Error("Failed to remove logic volume:", err)
		return err
	}
  */
	return nil, nil
}
