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
}

func NewConfiguration() *Configuration {
	return &Configuration{
		ConnectionTimeout:     time.Second * 17,
		DisableKeepAlives:     false,
		DisableCompression:    true,
		ResponseHeaderTimeout: time.Second * 17,
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
