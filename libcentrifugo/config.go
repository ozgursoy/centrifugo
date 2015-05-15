package libcentrifugo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/centrifugal/centrifugo/libcentrifugo/logger"
	"github.com/nu7hatch/gouuid"
	"github.com/spf13/viper"
)

type config struct {
	// name of this node - provided explicitly by configuration option
	// or constructed from hostname and port
	name string
	// admin password
	password string
	// secret key to generate auth token for admin
	secret string

	// prefix before each channel
	channelPrefix string
	// channel name for admin messages
	adminChannel string
	// channel name for internal control messages between nodes
	controlChannel string

	// in seconds, how often node must send ping control message
	nodePingInterval int64
	// in seconds, how often node must clean information about other running nodes
	nodeInfoCleanInterval int64
	// in seconds, how many seconds node info considered actual
	nodeInfoMaxDelay int64

	// in seconds, how often connected clients must update presence info
	presencePingInterval int64
	// in seconds, how long to consider presence info valid after receiving presence ping
	presenceExpireInterval int64

	// in seconds, an interval given to client to refresh its connection in the end of
	// connection lifetime
	expiredConnectionCloseDelay int64

	// prefix in channel name which indicates that channel is private
	privateChannelPrefix string
	// string separator which must be put after namespace part in channel name
	namespaceChannelBoundary string
	// string separator which must be set before allowed users part in channel name
	userChannelBoundary string
	// separates allowed users in user part of channel name
	userChannelSeparator string

	// insecure turns on insecure mode - when it's turned on then no authentication
	// required at all when connecting to Centrifugo, anonymous access and publish
	// allowed for all channels, no connection check performed. This can be suitable
	// for demonstration or personal usage
	insecure bool
}

// getApplicationName returns a name for this node. If no name provided
// in configuration then it constructs node name based on hostname and port
func getApplicationName() string {
	name := viper.GetString("name")
	if name != "" {
		return name
	}
	port := viper.GetString("port")
	var hostname string
	hostname, err := os.Hostname()
	if err != nil {
		logger.ERROR.Println(err)
		hostname = "?"
	}
	return hostname + "_" + port
}

func newConfig() *config {
	cfg := &config{}
	cfg.name = getApplicationName()
	cfg.password = viper.GetString("password")
	cfg.secret = viper.GetString("secret")
	cfg.channelPrefix = viper.GetString("channel_prefix")
	cfg.adminChannel = cfg.channelPrefix + "." + "admin"
	cfg.controlChannel = cfg.channelPrefix + "." + "control"
	cfg.nodePingInterval = int64(viper.GetInt("node_ping_interval"))
	cfg.nodeInfoCleanInterval = cfg.nodePingInterval * 3
	cfg.nodeInfoMaxDelay = cfg.nodePingInterval*2 + 1
	cfg.presencePingInterval = int64(viper.GetInt("presence_ping_interval"))
	cfg.presenceExpireInterval = int64(viper.GetInt("presence_expire_interval"))
	cfg.privateChannelPrefix = viper.GetString("private_channel_prefix")
	cfg.namespaceChannelBoundary = viper.GetString("namespace_channel_boundary")
	cfg.userChannelBoundary = viper.GetString("user_channel_boundary")
	cfg.userChannelSeparator = viper.GetString("user_channel_separator")
	cfg.expiredConnectionCloseDelay = int64(viper.GetInt("expired_connection_close_delay"))
	cfg.insecure = viper.GetBool("insecure")
	return cfg
}

// exists returns whether the given file or directory exists or not
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

var jsonConfigTemplate = `{
  "projects": [
    {
      "name": "{{.Name}}",
      "secret": "{{.Secret}}"
    }
  ]
}
`

var tomlConfigTemplate = `[[projects]]
    name = {{.Name}}
    secret = {{.Secret}}
`

var yamlConfigTemplate = `projects:
  - name: {{.Name}}
    secret: {{.Secret}}
`

func generateConfig(f string) error {
	exists, err := pathExists(f)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("output config file already exists: " + f)
	}
	ext := filepath.Ext(f)

	if len(ext) > 1 {
		ext = ext[1:]
	}

	supportedExts := []string{"json", "toml", "yaml", "yml"}

	if !stringInSlice(ext, supportedExts) {
		return errors.New("output config file must have one of supported extensions: " + strings.Join(supportedExts, ", "))
	}

	uid, err := uuid.NewV4()
	if err != nil {
		return err
	}

	var t *template.Template

	switch ext {
	case "json":
		t, err = template.New("config").Parse(jsonConfigTemplate)
	case "toml":
		t, err = template.New("config").Parse(tomlConfigTemplate)
	case "yaml", "yml":
		t, err = template.New("config").Parse(yamlConfigTemplate)
	}
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your project name: ")
	name, _, err := reader.ReadLine()
	if err != nil {
		return err
	}

	var output bytes.Buffer
	t.Execute(&output, struct {
		Name   string
		Secret string
	}{
		strings.Trim(string(name), " "),
		uid.String(),
	})

	err = ioutil.WriteFile(f, output.Bytes(), 0644)
	if err != nil {
		return err
	}

	err = validateConfig(f)
	if err != nil {
		_ = os.Remove(f)
		return err
	}

	return nil
}

func validateConfig(f string) error {
	v := viper.New()
	v.SetConfigFile(f)
	err := v.ReadInConfig()
	if err != nil {
		return errors.New("unable to locate config file")
	}
	structure := structureFromConfig(v)
	return structure.validate()
}

func structureFromConfig(v *viper.Viper) *structure {
	var pl projectList
	if v == nil {
		viper.MarshalKey("projects", &pl)
	} else {
		v.MarshalKey("projects", &pl)
	}
	s := &structure{
		ProjectList: pl,
	}
	s.initialize()
	return s
}