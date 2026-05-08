package webhdfs

import "fmt"
import "errors"
import "time"
import "net/url"

const WebHdfsVer string = "/webhdfs/v1"

type Configuration struct {
	Addr                  string // host:port
	BasePath              string // initial base path to be appended
	User                  string // user.name to use to connect
	ConnectionTimeout     time.Duration
	DisableKeepAlives     bool
	DisableCompression    bool
	ResponseHeaderTimeout time.Duration
	// ResolveDataNodeHostnames rewrites WebHDFS DataNode redirect hosts using
	// NameNode JMX LiveNodes, avoiding reliance on local hosts/DNS entries.
	ResolveDataNodeHostnames bool
	// DataNodeHostMap optionally provides explicit DataNode hostname to IP/host
	// mappings. It is used before fetching mappings from NameNode JMX.
	DataNodeHostMap map[string]string
	// DataNodeHostMapTTL controls how long JMX-derived DataNode mappings are
	// cached. If unset, one minute is used.
	DataNodeHostMapTTL time.Duration
	// DisableChecksumVerification disables the default local-vs-HDFS checksum
	// verification used by Create and FsShell upload/download helpers.
	DisableChecksumVerification bool
	// ChecksumWorkers controls parallel workers for local file checksum
	// calculation. If unset, four workers are used.
	ChecksumWorkers int
}

func NewConfiguration() *Configuration {
	return &Configuration{
		ConnectionTimeout:     time.Second * 17,
		DisableKeepAlives:     false,
		DisableCompression:    true,
		ResponseHeaderTimeout: time.Second * 17,
		DataNodeHostMapTTL:    time.Minute,
	}
}

func (conf *Configuration) GetNameNodeUrl() (*url.URL, error) {
	if conf.Addr == "" {
		return nil, errors.New("Configuration namenode address not set.")
	}

	if conf.User == "" {
		return nil, errors.New("User is not set")
	}

	var urlStr string = fmt.Sprintf("http://%s%s%s", conf.Addr, WebHdfsVer, conf.BasePath)
	urlStr = urlStr + "?user.name=" + conf.User

	u, err := url.Parse(urlStr)

	if err != nil {
		return nil, err
	}

	return u, nil
}
