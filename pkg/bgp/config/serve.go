package config

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type BgpConfigSet struct {
	Global            Global             `mapstructure:"global"`
	Neighbors         []Neighbor         `mapstructure:"neighbors"`
	PeerGroups        []PeerGroup        `mapstructure:"peer-groups"`
	RpkiServers       []RpkiServer       `mapstructure:"rpki-servers"`
	BmpServers        []BmpServer        `mapstructure:"bmp-servers"`
	Vrfs              []Vrf              `mapstructure:"vrfs"`
	MrtDump           []Mrt              `mapstructure:"mrt-dump"`
	Collector         Collector          `mapstructure:"collector"`
	DefinedSets       DefinedSets        `mapstructure:"defined-sets"`
	PolicyDefinitions []PolicyDefinition `mapstructure:"policy-definitions"`
	DynamicNeighbors  []DynamicNeighbor  `mapstructure:"dynamic-neighbors"`
	PorterConfig      PorterConfig       `mapstructure:"porter-config"`
}

func ReadConfigfile(v *viper.Viper) (*BgpConfigSet, error) {
	c := &BgpConfigSet{}
	var err error
	if err = v.ReadInConfig(); err != nil {
		return nil, err
	}
	if err = v.UnmarshalExact(c); err != nil {
		return nil, err
	}
	if err = setDefaultConfigValuesWithViper(v, c); err != nil {
		return nil, err
	}
	return c, nil
}

func ReadConfigfileServe(path, format string, configCh chan *BgpConfigSet) {
	sigReloadConfig := make(chan os.Signal)
	signal.Notify(sigReloadConfig, syscall.SIGHUP)

	// Update config file type, if detectable
	format = detectConfigFileType(path, format)
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType(format)
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		log.WithField("file", e.Name).Warn("Config file changed")
		sigReloadConfig <- syscall.SIGUSR1
	})
	cnt, notFoundCnt := 0, 0

	for {
		c, err := ReadConfigfile(v)
		if err != nil {
			if os.IsNotExist(err) {
				log.WithField("file", path).Warn("config file not loaded")
				notFoundCnt++
				if notFoundCnt < 5 {
					time.Sleep(5)
					continue
				}
				log.WithField("file", path).Error("config file not loaded(max times exceed)")
			}
			goto ERROR
		}
		if cnt == 0 {
			log.WithFields(log.Fields{
				"Topic": "Config",
			}).Info("Finished reading the config file")
		}
		cnt++
		configCh <- c
		goto NEXT
	ERROR:
		if cnt == 0 {
			log.WithFields(log.Fields{
				"Topic": "Config",
				"Error": err,
			}).Fatalf("Can't read config file %s", path)
		} else {
			log.WithFields(log.Fields{
				"Topic": "Config",
				"Error": err,
			}).Warningf("Can't reload config file %s", path)
		}
	NEXT:
		<-sigReloadConfig
		log.WithFields(log.Fields{
			"Topic": "Config",
		}).Info("Reload the config file")
	}
}

func ConfigSetToRoutingPolicy(c *BgpConfigSet) *RoutingPolicy {
	return &RoutingPolicy{
		DefinedSets:       c.DefinedSets,
		PolicyDefinitions: c.PolicyDefinitions,
	}
}

func UpdatePeerGroupConfig(curC, newC *BgpConfigSet) ([]PeerGroup, []PeerGroup, []PeerGroup) {
	addedPg := []PeerGroup{}
	deletedPg := []PeerGroup{}
	updatedPg := []PeerGroup{}

	for _, n := range newC.PeerGroups {
		if idx := existPeerGroup(n.Config.PeerGroupName, curC.PeerGroups); idx < 0 {
			addedPg = append(addedPg, n)
		} else if !n.Equal(&curC.PeerGroups[idx]) {
			log.WithFields(log.Fields{
				"Topic": "Config",
			}).Debugf("Current peer-group config:%v", curC.PeerGroups[idx])
			log.WithFields(log.Fields{
				"Topic": "Config",
			}).Debugf("New peer-group config:%v", n)
			updatedPg = append(updatedPg, n)
		}
	}

	for _, n := range curC.PeerGroups {
		if existPeerGroup(n.Config.PeerGroupName, newC.PeerGroups) < 0 {
			deletedPg = append(deletedPg, n)
		}
	}
	return addedPg, deletedPg, updatedPg
}

func UpdateNeighborConfig(curC, newC *BgpConfigSet) ([]Neighbor, []Neighbor, []Neighbor) {
	added := []Neighbor{}
	deleted := []Neighbor{}
	updated := []Neighbor{}

	for _, n := range newC.Neighbors {
		if idx := inSlice(n, curC.Neighbors); idx < 0 {
			added = append(added, n)
		} else if !n.Equal(&curC.Neighbors[idx]) {
			log.WithFields(log.Fields{
				"Topic": "Config",
			}).Debugf("Current neighbor config:%v", curC.Neighbors[idx])
			log.WithFields(log.Fields{
				"Topic": "Config",
			}).Debugf("New neighbor config:%v", n)
			updated = append(updated, n)
		}
	}

	for _, n := range curC.Neighbors {
		if inSlice(n, newC.Neighbors) < 0 {
			deleted = append(deleted, n)
		}
	}
	return added, deleted, updated
}

func CheckPolicyDifference(currentPolicy *RoutingPolicy, newPolicy *RoutingPolicy) bool {

	log.WithFields(log.Fields{
		"Topic": "Config",
	}).Debugf("Current policy:%v", currentPolicy)
	log.WithFields(log.Fields{
		"Topic": "Config",
	}).Debugf("New policy:%v", newPolicy)

	var result bool
	if currentPolicy == nil && newPolicy == nil {
		result = false
	} else {
		if currentPolicy != nil && newPolicy != nil {
			result = !currentPolicy.Equal(newPolicy)
		} else {
			result = true
		}
	}
	return result
}
