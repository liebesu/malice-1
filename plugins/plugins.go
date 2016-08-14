package plugins

import (
	"bytes"
	"fmt"

	"os"

	"github.com/BurntSushi/toml"
	log "github.com/Sirupsen/logrus"
	runconfigopts "github.com/docker/docker/runconfig/opts"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/strslice"
	"github.com/maliceio/malice/config"
	er "github.com/maliceio/malice/malice/errors"
	"github.com/maliceio/malice/malice/maldocker"
	"github.com/parnurzeal/gorequest"
)

// StartPlugin starts plugin
func (plugin Plugin) StartPlugin(client *maldocker.Docker, sample string, logs bool) (types.ContainerJSONBase, error) {

	binds := []string{config.Conf.Docker.Binds}
	env := plugin.getPluginEnv()

	contJSON, err := client.StartContainer(
		strslice.StrSlice{"-t", sample},
		plugin.Name,
		plugin.Image,
		logs,
		binds,
		nil,
		[]string{"rethink:rethink"},
		env,
	)
	er.CheckError(err)

	return contJSON, err
}

// RunIntelPlugins run all Intel plugins
func RunIntelPlugins(client *maldocker.Docker, hash string, scanID string, logs bool) {
	var cont types.ContainerJSONBase
	var err error
	for _, plugin := range GetIntelPlugins(true) {

		env := plugin.getPluginEnv()
		env = append(env, "MALICE_SCANID="+scanID)

		if plugin.Cmd != "" {
			cont, err = client.StartContainer(
				strslice.StrSlice{"-t", plugin.Cmd, hash},
				plugin.Name,
				plugin.Image,
				logs,
				nil,
				nil,
				[]string{"rethink"},
				env,
			)
			er.CheckError(err)
		} else {
			cont, err = client.StartContainer(
				strslice.StrSlice{"-t", hash},
				plugin.Name,
				plugin.Image,
				logs,
				nil,
				nil,
				[]string{"rethink"},
				env,
			)
			er.CheckError(err)
		}

		log.WithFields(log.Fields{
			"id": cont.ID,
			"ip": client.GetIP(),
			// "url":      "http://" + maldocker.GetIP(),
			"name": cont.Name,
			"env":  config.Conf.Environment.Run,
		}).Debug("Plugin Container Started")

		client.RemoveContainer(cont, true, true, true)
	}
}

func (plugin *Plugin) getPluginEnv() []string {
	var env []string
	for _, pluginEnv := range plugin.Env {
		env = append(env, fmt.Sprintf("%s=%s", pluginEnv, os.Getenv(pluginEnv)))
	}
	return env
}

func printStatus(resp gorequest.Response, body string, errs []error) {
	fmt.Println(resp.Status)
}

// PostResults post plugin results to Malice Webhook
func PostResults(url string, resultJSON []byte, taskID string) {
	request := gorequest.New()
	if config.Conf.Proxy.Enable {
		request = gorequest.New().Proxy(config.Conf.Proxy.HTTP)
	}
	request.Post(url).
		Set("Task", taskID).
		Send(resultJSON).
		End(printStatus)
}

//InstallPlugin installs a new malice plugin
func InstallPlugin(plugin *Plugin) (err error) {

	var newPlugin = Configuration{
		[]Plugin{
			Plugin{
				Name:        plugin.Name,
				Enabled:     plugin.Enabled,
				Category:    plugin.Category,
				Description: plugin.Description,
				Image:       plugin.Image,
				Mime:        plugin.Mime,
			},
		},
	}

	buf := new(bytes.Buffer)
	er.CheckError(toml.NewEncoder(buf).Encode(newPlugin))
	fmt.Println(buf.String())
	// open plugin config file
	f, err := os.OpenFile("./plugins.toml", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	// write new plugin to installed plugin config
	if _, err = f.WriteString("\n" + buf.String()); err != nil {
		panic(err)
	}
	return
}

// InstalledPluginsCheck checks that all enabled plugins are installed
func InstalledPluginsCheck(client *maldocker.Docker) bool {
	for _, plugin := range getEnabled(Plugs.Plugins) {
		if _, exists, _ := client.ImageExists(plugin.Image); !exists {
			return false
		}
	}
	return true
}

// UpdatePlugin performs a docker pull on all registered plugins checking for updates
func (plugin Plugin) UpdatePlugin(client *maldocker.Docker) {
	client.PullImage(plugin.Image, "latest")
}

// UpdatePluginFromRepository performs a docker build on a plugins remote repository
func (plugin Plugin) UpdatePluginFromRepository(client *maldocker.Docker) {

	log.Info("[Building Plugin from Source] ===> ", plugin.Name)

	var buildArgs map[string]string
	var quiet = false

	tags := []string{"malice/" + plugin.Name + ":latest"}

	if config.Conf.Proxy.Enable {
		buildArgs = runconfigopts.ConvertKVStringsToMap([]string{
			"HTTP_PROXY=" + config.Conf.Proxy.HTTP,
			"HTTPS_PROXY=" + config.Conf.Proxy.HTTPS,
		})
	} else {
		buildArgs = nil
	}

	labels := runconfigopts.ConvertKVStringsToMap([]string{"io.malice.plugin.installed.from=repository"})

	client.BuildImage(plugin.Repository, tags, buildArgs, labels, quiet)
}

// UpdateAllPlugins performs a docker pull on all registered plugins checking for updates
func UpdateAllPlugins(client *maldocker.Docker) {
	plugins := Plugs.Plugins
	for _, plugin := range plugins {
		fmt.Println("[Updating Plugin] ===> ", plugin.Name)
		client.PullImage(plugin.Image, "latest")
	}
}

// UpdateAllPluginsFromSource performs a docker build on a plugins remote repository on all registered plugins
func UpdateAllPluginsFromSource(client *maldocker.Docker) {
	plugins := Plugs.Plugins
	for _, plugin := range plugins {
		fmt.Println("[Updating Plugin from Source] ===> ", plugin.Name)
		plugin.UpdatePluginFromRepository(client)
	}
}
