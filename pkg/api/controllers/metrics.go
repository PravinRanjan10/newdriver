// Copyright (c) 2019 The OpenSDS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
This module implements a entry into the OpenSDS northbound service.

*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"

	log "github.com/golang/glog"
	"github.com/opensds/opensds/pkg/api/policy"
	c "github.com/opensds/opensds/pkg/context"
	"github.com/opensds/opensds/pkg/controller/client"
	"github.com/opensds/opensds/pkg/model"
	pb "github.com/opensds/opensds/pkg/model/proto"
	. "github.com/opensds/opensds/pkg/utils/config"
)

// prometheus constants
var PrometheusConfHome string
var PrometheusUrl string
var PrometheusConfFile string

// alert manager constants
var AlertmgrConfHome string
var AlertmgrUrl string
var AlertmgrConfFile string

var GrafanaConfHome string
var GrafanaRestartCmd string
var GrafanaConfFile string

var ReloadPath string
var BackupExtension string

func init() {

	// TODO Prakash read these from conf and save to these variables
	ReloadPath = "/-/reload"
	BackupExtension = ".bak"

	PrometheusConfHome = "/etc/prometheus/"
	PrometheusUrl = "http://localhost:9090"
	PrometheusConfFile = "prometheus.yml"

	AlertmgrConfHome = "/root/alertmanager-0.16.2.linux-amd64/"
	AlertmgrUrl = "http://localhost:9093"
	AlertmgrConfFile = "alertmanager.yml"

	GrafanaConfHome = "/etc/grafana/"
	GrafanaRestartCmd = "grafana-server"
	GrafanaConfFile = "grafana.ini"
}

func NewMetricsPortal() *MetricsPortal {
	return &MetricsPortal{
		CtrClient: client.NewClient(),
	}
}

type MetricsPortal struct {
	BasePortal

	CtrClient client.Client
}

func (m *MetricsPortal) GetMetrics() {
	if !policy.Authorize(m.Ctx, "metrics:get") {
		return
	}
	ctx := c.GetContext(m.Ctx)
	var getMetricSpec = model.GetMetricSpec{
		BaseModel: &model.BaseModel{},
	}

	// Unmarshal the request body
	if err := json.NewDecoder(m.Ctx.Request.Body).Decode(&getMetricSpec); err != nil {
		errMsg := fmt.Sprintf("parse get metric request body failed: %s", err.Error())
		m.ErrorHandle(model.ErrorBadRequest, errMsg)
		return
	}

	if err := m.CtrClient.Connect(CONF.OsdsLet.ApiEndpoint); err != nil {
		log.Error("when connecting controller client:", err)
		return
	}
	defer m.CtrClient.Close()

	opt := &pb.GetMetricsOpts{
		InstanceId: getMetricSpec.InstanceId,
		MetricName: getMetricSpec.MetricName,
		StartTime:  getMetricSpec.StartTime,
		EndTime:    getMetricSpec.EndTime,
		Context:    ctx.ToJson(),
	}
	res, err := m.CtrClient.GetMetrics(context.Background(), opt)

	if err != nil {
		log.Error("collect metrics failed in controller service:", err)
		return
	}

	m.SuccessHandle(StatusOK, []byte(res.GetResult().GetMessage()))

	return
}

func (m *MetricsPortal) UploadConfFile() {

	params, _ := m.GetParameters()
	confType := params["conftype"][0]

	switch confType {
	case "prometheus":
		DoUpload(m, PrometheusConfHome, PrometheusUrl, ReloadPath, true)
	case "alertmanager":
		DoUpload(m, AlertmgrConfHome, AlertmgrUrl, ReloadPath, true)
	case "grafana":
		// for grafana, there is no reload endpoint to call
		DoUpload(m, GrafanaConfHome, "", "", false)
		// to reload the configuration, run the reload command for grafana
		cmd := exec.Command("systemctl", "restart", GrafanaRestartCmd)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Fatalf("restart grafana failed with %s\n", err)
		}
		return

	}
}

func DoUpload(metricsPortal *MetricsPortal, confHome string, url string, reloadPath string, toCallReloadEndpoint bool) {

	// get the uploaded file
	f, h, _ := metricsPortal.GetFile("conf_file")

	// get the path to save the configuration
	path := confHome + h.Filename

	// close the incoming file
	fCloseErr := f.Close()
	if fCloseErr != nil {
		log.Errorf("error closing uploaded file %s", h.Filename)
		metricsPortal.ErrorHandle(model.ErrorInternalServer, fCloseErr.Error())
		return
	}

	// backup the current configuration file
	_, currErr := os.Stat(path)

	// make backup path
	backupPath := path + BackupExtension

	if currErr == nil {
		// current configuration exists, back it up
		fRenameErr := os.Rename(path, backupPath)
		if fRenameErr != nil {
			log.Errorf("error renaming file %s to %s", path, backupPath)
			metricsPortal.ErrorHandle(model.ErrorInternalServer, fRenameErr.Error())
			return
		}
	}

	// save file to disk
	fSaveErr := metricsPortal.SaveToFile("conf_file", path)
	if fSaveErr != nil {
		log.Errorf("error saving file %s", path)
	} else {
		if toCallReloadEndpoint == true {
			reloadResp, reloadErr := http.Post(url+reloadPath, "application/json", nil)
			if reloadErr != nil {
				log.Errorf("error on reload of configuration %s", reloadErr)
				metricsPortal.ErrorHandle(model.ErrorInternalServer, reloadErr.Error())
				return
			}
			respBody, readBodyErr := ioutil.ReadAll(reloadResp.Body)
			if readBodyErr != nil {
				log.Errorf("error on reload of configuration %s", reloadErr)
				metricsPortal.ErrorHandle(model.ErrorInternalServer, readBodyErr.Error())
				return
			}
			metricsPortal.SuccessHandle(StatusOK, respBody)
			return
		}
		metricsPortal.SuccessHandle(StatusOK, nil)
		return
	}
}

func (m *MetricsPortal) DownloadConfFile() {

	params, _ := m.GetParameters()
	confType := params["conftype"][0]

	switch confType {
	case "prometheus":
		DoDownload(m, PrometheusConfHome, PrometheusConfFile)
	case "alertmanager":
		DoDownload(m, AlertmgrConfHome, AlertmgrConfFile)
	case "grafana":
		DoDownload(m, GrafanaConfHome, GrafanaConfFile)
	}
}

func DoDownload(metricsPortal *MetricsPortal, confHome string, confFile string) {
	// get the path to the configuration file
	path := confHome + confFile
	// check, if file exists
	_, currErr := os.Stat(path)
	if currErr != nil && os.IsNotExist(currErr) {
		log.Errorf("file %s not found", path)
		metricsPortal.ErrorHandle(model.ErrorNotFound, currErr.Error())
		return
	}
	// file exists, download it
	metricsPortal.Ctx.Output.Download(path, path)
}
