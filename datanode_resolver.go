package webhdfs

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type nameNodeInfoJMX struct {
	Beans []struct {
		LiveNodes string
	}
}

type liveNodeInfo struct {
	InfoAddr string `json:"infoAddr"`
	XferAddr string `json:"xferaddr"`
}

func (fs *FileSystem) resolveDataNodeURL(location string) (string, error) {
	if !fs.Config.ResolveDataNodeHostnames && len(fs.Config.DataNodeHostMap) == 0 {
		return location, nil
	}

	u, err := url.Parse(location)
	if err != nil {
		return "", err
	}

	host := u.Hostname()
	if host == "" {
		return location, nil
	}

	hostMap, err := fs.getDataNodeHostMap()
	if err != nil {
		return "", err
	}

	resolvedHost := hostMap[host]
	if resolvedHost == "" || resolvedHost == host {
		return location, nil
	}

	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(resolvedHost, port)
	} else {
		u.Host = resolvedHost
	}
	return u.String(), nil
}

func (fs *FileSystem) getDataNodeHostMap() (map[string]string, error) {
	if len(fs.Config.DataNodeHostMap) > 0 {
		return fs.Config.DataNodeHostMap, nil
	}
	if !fs.Config.ResolveDataNodeHostnames {
		return nil, nil
	}

	fs.dataNodeMu.Lock()
	defer fs.dataNodeMu.Unlock()

	ttl := fs.Config.DataNodeHostMapTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	if fs.dataNodeHostMap != nil && time.Since(fs.dataNodeMapAt) < ttl {
		return fs.dataNodeHostMap, nil
	}

	hostMap, err := fetchDataNodeHostMap(fs.Config)
	if err != nil {
		return nil, err
	}
	fs.dataNodeHostMap = hostMap
	fs.dataNodeMapAt = time.Now()
	return hostMap, nil
}

func fetchDataNodeHostMap(conf Configuration) (map[string]string, error) {
	timeout := conf.ResponseHeaderTimeout
	if timeout <= 0 {
		timeout = conf.ConnectionTimeout
	}
	if timeout <= 0 {
		timeout = 17 * time.Second
	}

	client := http.Client{Timeout: timeout}
	jmxURL := fmt.Sprintf("http://%s/jmx?qry=Hadoop:service=NameNode,name=NameNodeInfo", conf.Addr)
	resp, err := client.Get(jmxURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NameNode JMX returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var jmx nameNodeInfoJMX
	if err := json.Unmarshal(body, &jmx); err != nil {
		return nil, err
	}
	if len(jmx.Beans) == 0 {
		return nil, fmt.Errorf("NameNode JMX returned no beans")
	}

	var liveNodes map[string]liveNodeInfo
	if err := json.Unmarshal([]byte(jmx.Beans[0].LiveNodes), &liveNodes); err != nil {
		return nil, err
	}

	hostMap := make(map[string]string, len(liveNodes))
	for node, info := range liveNodes {
		nodeHost := hostWithoutPort(node)
		ipHost := hostWithoutPort(info.XferAddr)
		if ipHost == "" {
			ipHost = hostWithoutPort(info.InfoAddr)
		}
		if nodeHost != "" && ipHost != "" {
			hostMap[nodeHost] = ipHost
		}
	}
	return hostMap, nil
}

func hostWithoutPort(value string) string {
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return host
	}
	if i := strings.LastIndex(value, ":"); i > -1 {
		return value[:i]
	}
	return value
}
