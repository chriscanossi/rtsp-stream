package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"sync"
	"time"

	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/deepch/vdk/codec/h265parser"
)

//Config global
var Config = loadConfig()

//ConfigST struct
type ConfigST struct {
	mutex   sync.RWMutex
	Server  ServerST            `json:"server"`
	Streams map[string]StreamST `json:"streams"`
	LastError error
}

//ServerST struct
type ServerST struct {
	HTTPPort      string   			`json:"encoderPort"`
	ICEServers    []string 			`json:"iceServers"`
	WebRTCPortMin uint16   			`json:"webrtcMinPort"`
	WebRTCPortMax uint16   			`json:"webrtcMaxPort"`
	RetryConnect  time.Duration `json:"retryConnectSec"`
}

//StreamST struct
type StreamST struct {
	URL          string `json:"url"`
	Status       bool   `json:"status"`
	OnDemand     bool   `json:"VOD"`
	DisableAudio bool   `json:"disableAudio"`
	Debug        bool   `json:"debug"`
	RunLock      bool   `json:"-"`
	Codecs       []av.CodecData
	Cl           map[string]viewer
}

type viewer struct {
	c chan av.Packet
}

func (element *ConfigST) RunIFNotRun(uuid string) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	if tmp, ok := element.Streams[uuid]; ok {
		if tmp.OnDemand && !tmp.RunLock {
			tmp.RunLock = true
			element.Streams[uuid] = tmp
			go RTSPWorkerLoop(uuid, tmp.URL, tmp.OnDemand, tmp.DisableAudio, tmp.Debug)
		}
	}
}

func (element *ConfigST) RunUnlock(uuid string) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	if tmp, ok := element.Streams[uuid]; ok {
		if tmp.OnDemand && tmp.RunLock {
			tmp.RunLock = false
			element.Streams[uuid] = tmp
		}
	}
}

func (element *ConfigST) HasViewer(uuid string) bool {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	if tmp, ok := element.Streams[uuid]; ok && len(tmp.Cl) > 0 {
		return true
	}
	return false
}

func (element *ConfigST) GetICEServers() []string {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	return element.Server.ICEServers
}

func (element *ConfigST) GetWebRTCPortMin() uint16 {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	return element.Server.WebRTCPortMin
}

func (element *ConfigST) GetWebRTCPortMax() uint16 {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	return element.Server.WebRTCPortMax
}

func loadConfig() *ConfigST {
	
	filePath, err := findUp("config.json")
	if err != nil {
		log.Fatalln(err)
	}

	var tmp ConfigST
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalln(err)
	}
	err = json.Unmarshal(data, &tmp)
	if err != nil {
		log.Fatalln(err)
	}
	for i, v := range tmp.Streams {
		v.Cl = make(map[string]viewer)
		tmp.Streams[i] = v
	}
	return &tmp
}

func (element *ConfigST) cast(uuid string, pck av.Packet) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	for _, v := range element.Streams[uuid].Cl {
		if len(v.c) < cap(v.c) {
			v.c <- pck
		}
	}
}

func (element *ConfigST) ext(suuid string) bool {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	_, ok := element.Streams[suuid]
	return ok
}

func (element *ConfigST) coAd(suuid string, codecs []av.CodecData) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	t := element.Streams[suuid]
	t.Codecs = codecs
	element.Streams[suuid] = t
}

func (element *ConfigST) coGe(suuid string) []av.CodecData {
	for i := 0; i < 100; i++ {
		element.mutex.RLock()
		tmp, ok := element.Streams[suuid]
		element.mutex.RUnlock()
		if !ok {
			return nil
		}
		
		if tmp.Codecs != nil {
			//TODO Delete test
			for _, codec := range tmp.Codecs {
				if codec.Type() == av.H264 {
					codecVideo := codec.(h264parser.CodecData)
					if codecVideo.SPS() != nil && codecVideo.PPS() != nil && len(codecVideo.SPS()) > 0 && len(codecVideo.PPS()) > 0 {
						//ok
						log.Println("H264 video detected")
					} else {
						//video codec not ok
						log.Println("unrecognized video codec")
						time.Sleep(50 * time.Millisecond)
						continue
					}
				} else if codec.Type() == av.H265 {
					codecVideo := codec.(h265parser.CodecData)
					if codecVideo.SPS() != nil && codecVideo.PPS() != nil && len(codecVideo.SPS()) > 0 && len(codecVideo.PPS()) > 0 {
						//ok
						log.Println("H256 video detected")
					} else {
						//video codec not ok
						log.Println("unrecognized video codec")
						time.Sleep(50 * time.Millisecond)
						continue
					}
				}
			}
			return tmp.Codecs
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func (element *ConfigST) clAd(suuid string) (string, chan av.Packet) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	cuuid := pseudoUUID()
	ch := make(chan av.Packet, 100)
	element.Streams[suuid].Cl[cuuid] = viewer{c: ch}
	return cuuid, ch
}

func (element *ConfigST) list() (string, []string) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	var res []string
	var fist string
	for k := range element.Streams {
		if fist == "" {
			fist = k
		}
		res = append(res, k)
	}
	return fist, res
}
func (element *ConfigST) clDe(suuid, cuuid string) {
	element.mutex.Lock()
	defer element.mutex.Unlock()
	delete(element.Streams[suuid].Cl, cuuid)
}

func pseudoUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return
}
