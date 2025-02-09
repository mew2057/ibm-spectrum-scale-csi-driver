/**
 * Copyright 2019 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scale

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/golang/glog"
	"github.com/container-storage-interface/spec/lib/go/csi"
        "k8s.io/kubernetes/pkg/util/mount"

        "github.com/IBM/ibm-spectrum-scale-csi-driver/csiplugin/connectors"
        "github.com/IBM/ibm-spectrum-scale-csi-driver/csiplugin/settings"
        "github.com/IBM/ibm-spectrum-scale-csi-driver/csiplugin/utils"
)

// PluginFolder defines the location of scaleplugin
const (
	PluginFolder      = "/var/lib/kubelet/plugins/csi-spectrum-scale"
)

type ScaleDriver struct {
	name          string
	vendorVersion string
	nodeID        string

	ids *ScaleIdentityServer
	ns  *ScaleNodeServer
	cs  *ScaleControllerServer

        connmap map[string]connectors.SpectrumScaleConnector
        cmap settings.ScaleSettingsConfigMap
        primary settings.Primary
	reqmap map[string]int64

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability
}

var scaleVolumes map[string]*scaleVolume

// Init checks for the persistent volume file and loads all found volumes
// into a memory structure
func init() {
        glog.V(3).Infof("gpfs init") 
	scaleVolumes = map[string]*scaleVolume{}
	if _, err := os.Stat(path.Join(PluginFolder, "controller")); os.IsNotExist(err) {
		glog.Infof("scale: folder %s not found. Creating... \n", path.Join(PluginFolder, "controller"))
		if err := os.Mkdir(path.Join(PluginFolder, "controller"), 0755); err != nil {
			glog.Fatalf("Failed to create a controller's volumes folder with error: %v\n", err)
		}
	} else {
		// Since "controller" folder exists, it means the plugin has already been running, it means
		// there might be some volumes left, they must be re-inserted into scaleVolumes map
		loadExVolumes()
	}
}


// loadExVolumes check for any *.json files in the  PluginFolder/controller folder
// and loads then into scaleVolumes map
func loadExVolumes() {
        glog.V(3).Infof("gpfs loadExVolumes")
	scaleVol := scaleVolume{}
	files, err := ioutil.ReadDir(path.Join(PluginFolder, "controller"))
	if err != nil {
		glog.Infof("scale: failed to read controller's volumes folder: %s error:%v", path.Join(PluginFolder, "controller"), err)
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		fp, err := os.Open(path.Join(PluginFolder, "controller", f.Name()))
		if err != nil {
			glog.Infof("scale: open file: %s err %%v", f.Name(), err)
			continue
		}
		decoder := json.NewDecoder(fp)
		if err = decoder.Decode(&scaleVol); err != nil {
			glog.Infof("scale: decode file: %s err: %v", f.Name(), err)
			fp.Close()
			continue
		}
/*		scaleVolumes[scaleVol.VolID] = &scaleVol */
	}
	glog.Infof("scale: Loaded %d volumes from %s", len(scaleVolumes), path.Join(PluginFolder, "controller"))
}

func GetScaleDriver() *ScaleDriver {
        glog.V(3).Infof("gpfs GetScaleDriver")
	return &ScaleDriver{}
}

func NewIdentityServer(d *ScaleDriver) *ScaleIdentityServer {
        glog.V(3).Infof("gpfs NewIdentityServer")
	return &ScaleIdentityServer{
		Driver: d,
	}
}

func NewControllerServer(d *ScaleDriver, connMap map[string]connectors.SpectrumScaleConnector, cmap settings.ScaleSettingsConfigMap, primary settings.Primary) *ScaleControllerServer {
	glog.V(3).Infof("gpfs NewControllerServer")
	d.connmap = connMap
        d.cmap = cmap
        d.primary = primary
	d.reqmap = make(map [string]int64)
	return &ScaleControllerServer{
		Driver: d,
	}
}

func NewNodeServer(d *ScaleDriver, mounter *mount.SafeFormatAndMount) *ScaleNodeServer {
        glog.V(3).Infof("gpfs NewNodeServer")
	return &ScaleNodeServer{
		Driver: d,
		Mounter: mounter,
	}
}

func (driver *ScaleDriver) AddVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) error {
        glog.V(3).Infof("gpfs AddVolumeCapabilityAccessModes")
	var vca []*csi.VolumeCapability_AccessMode
	for _, c := range vc {
		glog.V(3).Infof("Enabling volume access mode: %v", c.String())
		vca = append(vca, NewVolumeCapabilityAccessMode(c))
	}
	driver.vcap = vca
	return nil
}

func (driver *ScaleDriver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) error {
        glog.V(3).Infof("gpfs AddControllerServiceCapabilities")
	var csc []*csi.ControllerServiceCapability
	for _, c := range cl {
		glog.V(3).Infof("Enabling controller service capability: %v", c.String())
		csc = append(csc, NewControllerServiceCapability(c))
	}
	driver.cscap = csc
	return nil
}

func (driver *ScaleDriver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) error {
        glog.V(3).Infof("gpfs AddNodeServiceCapabilities")
	var nsc []*csi.NodeServiceCapability
	for _, n := range nl {
		glog.V(3).Infof("Enabling node service capability: %v", n.String())
		nsc = append(nsc, NewNodeServiceCapability(n))
	}
	driver.nscap = nsc
	return nil
}

func (driver *ScaleDriver) ValidateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
        glog.V(3).Infof("gpfs ValidateControllerServiceRequest")
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}
	for _, cap := range driver.cscap {
		if c == cap.GetRpc().Type {
			return nil
		}
	}
	return status.Error(codes.InvalidArgument, "Invalid controller service request")
}

func (driver *ScaleDriver) SetupScaleDriver(name, vendorVersion, nodeID string, mounter *mount.SafeFormatAndMount) error {
        glog.V(3).Infof("gpfs SetupScaleDriver. name: %s, version: %v, nodeID: %s", name, vendorVersion, nodeID)
	if name == "" {
		return fmt.Errorf("Driver name missing")
	}

        scmap, cmap, primary, err := driver.PluginInitialize()
        if err != nil {
                glog.Errorf("Error in plugin initialization: %s", err)
                return err
        }

	driver.name = name
	driver.vendorVersion = vendorVersion
	driver.nodeID = nodeID

	// Adding Capabilities
	vcam := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}
	driver.AddVolumeCapabilityAccessModes(vcam)

	csc := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
	}
	driver.AddControllerServiceCapabilities(csc)

	ns := []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
	driver.AddNodeServiceCapabilities(ns)

	driver.ids = NewIdentityServer(driver)
	driver.ns = NewNodeServer(driver, mounter)
	driver.cs = NewControllerServer(driver, scmap, cmap, primary)
	return nil
}

func (driver *ScaleDriver) PluginInitialize() (map [string]connectors.SpectrumScaleConnector, settings.ScaleSettingsConfigMap, settings.Primary, error) {
        glog.V(3).Infof("gpfs PluginInitialize")
        scaleConfig := settings.LoadScaleConfigSettings()

        isValid, err := driver.ValidateScaleConfigParameters(scaleConfig)
        if !isValid {
                glog.Errorf("Parameter validation failure")
                return nil, settings.ScaleSettingsConfigMap{}, settings.Primary{}, err
        }

        scaleConnMap := make(map [string]connectors.SpectrumScaleConnector)
	primaryInfo := settings.Primary{}

        for i := 0; i < len(scaleConfig.Clusters); i++ {
            cluster := scaleConfig.Clusters[i]

            sc, err := connectors.GetSpectrumScaleConnector(cluster)
            if err != nil {
                glog.Errorf("Unable to initialize Spectrum Scale connector for cluster %s", cluster.ID)
                return nil, scaleConfig, primaryInfo, err
            }

            // validate cluster ID
            clusterId, err := sc.GetClusterId()
            if err != nil {
                glog.Errorf("Error getting cluster ID: %v", err)
                return nil, scaleConfig, primaryInfo, err
            }
            if cluster.ID != clusterId {
                glog.Errorf("Cluster ID %s from scale config doesnt match the ID from cluster %s.", cluster.ID, clusterId)
                return nil, scaleConfig, primaryInfo, fmt.Errorf("Cluster ID doesnt match the cluster")
            }

            scaleConnMap[clusterId] = sc

            if cluster.Primary != (settings.Primary{}) {

		scaleConnMap["primary"] = sc

                // check if primary filesystem exists and mounted on atleast one node
                fsMount, err := sc.GetFilesystemMountDetails(cluster.Primary.PrimaryFS)
                if err != nil {
                    glog.Errorf("Error in getting filesystem details for %s", cluster.Primary.PrimaryFS)
                    return nil, scaleConfig, cluster.Primary, err
                }
                if fsMount.NodesMounted == nil || len(fsMount.NodesMounted) == 0 {
                    return nil, scaleConfig, cluster.Primary, fmt.Errorf("Primary filesystem not mounted on any node")
                }

                scaleConfig.Clusters[i].Primary.PrimaryFSMount = fsMount.MountPoint
                scaleConfig.Clusters[i].Primary.PrimaryCid = clusterId

		primaryInfo = scaleConfig.Clusters[i].Primary
           }
        }

	fs := primaryInfo.PrimaryFS
	sconn := scaleConnMap["primary"]
        fsmount := primaryInfo.PrimaryFSMount
	if primaryInfo.RemoteCluster != "" {
		sconn = scaleConnMap[primaryInfo.RemoteCluster]
		if primaryInfo.RemoteFS != "" {
		    fs = primaryInfo.RemoteFS

                    // check if primary filesystem exists on remote cluster and mounted on atleast one node
                    fsMount, err := sconn.GetFilesystemMountDetails(fs)
                    if err != nil {
                        glog.Errorf("Error in getting filesystem details for %s from cluster %s", fs, primaryInfo.RemoteCluster)
                        return scaleConnMap, scaleConfig, primaryInfo, err
                    }
		    glog.Infof("remote fsMount = %v", fsMount)
                    if fsMount.NodesMounted == nil || len(fsMount.NodesMounted) == 0 {
                        return scaleConnMap, scaleConfig, primaryInfo, fmt.Errorf("Primary filesystem not mounted on any node on cluster %s", primaryInfo.RemoteCluster)
                    }
		    fsmount = fsMount.MountPoint
		}
	} 

	fsetlinkpath, err := driver.CreatePrimaryFileset(sconn, fs, fsmount, primaryInfo.PrimaryFset, primaryInfo.InodeLimit)
	if err != nil {
		glog.Errorf("Error in creating primary fileset")
		return scaleConnMap, scaleConfig, primaryInfo, err
	}

	if fsmount != primaryInfo.PrimaryFSMount {
		fsetlinkpath = strings.Replace(fsetlinkpath, fsmount, primaryInfo.PrimaryFSMount, 1)
	}

        // Validate hostpath from daemonset is valid
        err = driver.ValidateHostpath(primaryInfo.PrimaryFSMount, fsetlinkpath)
        if err != nil {
        glog.Errorf("Hostpath validation failed")
                return scaleConnMap, scaleConfig, primaryInfo, err
        }

        // Create directory where volume symlinks will reside
        symlinkPath, relativePath, err := driver.CreateSymlinkPath(scaleConnMap["primary"], primaryInfo.PrimaryFS, primaryInfo.PrimaryFSMount, fsetlinkpath)
        if err != nil {
                glog.Errorf("Error in creating volumes directory")
                return scaleConnMap, scaleConfig, primaryInfo, err
        }   
        primaryInfo.SymlinkAbsolutePath = symlinkPath
        primaryInfo.SymlinkRelativePath = relativePath
        primaryInfo.PrimaryFsetLink = fsetlinkpath

        glog.Infof("IBM Spectrum Scale: Plugin initialized")
        return scaleConnMap, scaleConfig, primaryInfo, nil

}

func (driver *ScaleDriver) CreatePrimaryFileset(sc connectors.SpectrumScaleConnector, primaryFS string, fsmount string, filesetName string, inodeLimit string) (string, error) {
        glog.V(4).Infof("gpfs CreatePrimaryFileset. primaryFS: %s, mountpoint: %s, filesetName: %s", primaryFS, fsmount, filesetName)

                // create primary fileset if not already created
                fsetResponse, err := sc.ListFileset(primaryFS, filesetName)
                linkpath := fsetResponse.Config.Path
		newlinkpath := path.Join(fsmount, filesetName)

                if err != nil {
                    glog.Infof("Primary fileset %s not found. Creating it.", filesetName)
                    opts := make(map[string]interface{})
		    if (inodeLimit  != "") {
                        opts[connectors.UserSpecifiedInodeLimit] = inodeLimit
       		    }
                    err = sc.CreateFileset(primaryFS, filesetName, opts)
                    if err != nil {
                        glog.Errorf("Unable to create primary fileset %s", filesetName)
                        return "", err
                    }
                    linkpath = newlinkpath
                } else if linkpath == "" || linkpath == "--" {
                    glog.Infof("Primary fileset %s not linked. Linking it.", filesetName)
                    err = sc.LinkFileset(primaryFS, filesetName, newlinkpath)
                    if err != nil {
                        glog.Errorf("Unable to link primary fileset %s", filesetName)
                        return "", err
                    } else {
                        glog.Infof("Linked primary fileset %s. Linkpath: %s", newlinkpath, filesetName)
                    }
                    linkpath = newlinkpath
                } else {
                    glog.Infof("Primary fileset %s exists and linked at %s", filesetName, linkpath)
                }

		return linkpath, nil
}

func (driver *ScaleDriver) CreateSymlinkPath(sc connectors.SpectrumScaleConnector, fs string, fsmount string, fsetlinkpath string) (string, string, error) {
        glog.V(4).Infof("gpfs CreateSymlinkPath. filesystem: %s, mountpoint: %s, filesetlinkpath: %s", fs, fsmount, fsetlinkpath)        

        dirpath := strings.Replace(fsetlinkpath, fsmount, "", 1)
        dirpath = strings.Trim(dirpath, "!/")
        fsetlinkpath = strings.TrimSuffix(fsetlinkpath, "/")
 
        dirpath = fmt.Sprintf("%s/.volumes", dirpath)
        symlinkpath := fmt.Sprintf("%s/.volumes", fsetlinkpath)

        err := sc.MakeDirectory(fs, dirpath, 0, 0)
        if err != nil {
            glog.Errorf("Make directory failed on filesystem %s, path = %s", fs, dirpath)
            return symlinkpath, dirpath, err
        }

        return symlinkpath, dirpath, nil
}

func (driver *ScaleDriver) ValidateHostpath(mountpath string, linkpath string) error {
        glog.V(4).Infof("gpfs ValidateHostpath. mountpath: %s, linkpath: %s", mountpath, linkpath)

	hostpath := utils.GetEnv("SCALE_HOSTPATH", "")
	if hostpath == "" {
		return fmt.Errorf("SCALE_HOSTPATH not defined in daemonset")
	}

	if !strings.HasSuffix(hostpath, "/") {
	    hostpathslice := []string{hostpath}
	    hostpathslice = append(hostpathslice, "/")
	    hostpath = strings.Join(hostpathslice, "")
	}

	if !strings.HasSuffix(linkpath, "/") {
	    linkpathslice := []string{linkpath}
	    linkpathslice = append(linkpathslice, "/")
	    linkpath = strings.Join(linkpathslice, "")
	}

	if !strings.HasSuffix(mountpath, "/") {
	    mountpathslice := []string{mountpath}
	    mountpathslice = append(mountpathslice, "/")
	    mountpath = strings.Join(mountpathslice, "")
	}

	if !strings.HasPrefix(hostpath, linkpath) &&
	   !strings.HasPrefix(hostpath, mountpath) &&
	   !strings.HasPrefix(linkpath, hostpath) &&
	   !strings.HasPrefix(mountpath, hostpath) {
	    return fmt.Errorf("Invalid SCALE_HOSTPATH")
	}

        return nil
}

func (driver *ScaleDriver) ValidateScaleConfigParameters(scaleConfig settings.ScaleSettingsConfigMap) (bool, error) {
        glog.V(4).Infof("gpfs ValidateScaleConfigParameters.")
        if len(scaleConfig.Clusters) == 0 {
                return false, fmt.Errorf("Missing cluster information in Spectrum Scale configuration")
        }

        primaryClusterFound := false
	rClusterForPrimaryFS := ""
	var cl []string = make([]string, len(scaleConfig.Clusters))

        for i := 0; i < len(scaleConfig.Clusters); i++ {
            cluster := scaleConfig.Clusters[i]

            if cluster.ID == "" || len(cluster.RestAPI) == 0 || cluster.RestAPI[0].GuiHost == "" {
                return false, fmt.Errorf("Mandatory parameters not specified for cluster %v", cluster.ID)
            }

            if cluster.Primary != (settings.Primary{}) {
                if primaryClusterFound == true {
                    return false, fmt.Errorf("More than one primary clusters specified")
                }

                primaryClusterFound = true

                if cluster.Primary.PrimaryFS == "" || cluster.Primary.PrimaryFset == "" {
                        return false, fmt.Errorf("Mandatory parameters not specified for primary cluster %v", cluster.ID)
                }

		rClusterForPrimaryFS = cluster.Primary.RemoteCluster
			
            } else {
		cl[i] = cluster.ID
	    }

            if cluster.Secrets == "" || cluster.MgmtUsername == "" || cluster.MgmtPassword == "" {
                return false, fmt.Errorf("Invalid secret specified for cluster %v", cluster.ID)
            }

	    if cluster.SecureSslMode && cluster.CacertValue == nil {
		return false, fmt.Errorf("CA certificate not specified in secure SSL mode for cluster %v", cluster.ID)
	    }
        }

        if !primaryClusterFound {
            return false, fmt.Errorf("No primary clusters specified")
        }

	if rClusterForPrimaryFS != "" && !utils.StringInSlice(rClusterForPrimaryFS, cl) {
	    return false, fmt.Errorf("Remote cluster specified for primary filesystem: %s, but no definition found for it in config", rClusterForPrimaryFS)
	}

        return true, nil
}

func (driver *ScaleDriver) Run(endpoint string) {
	glog.Infof("Driver: %v version: %v", driver.name, driver.vendorVersion)
	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, driver.ids, driver.cs, driver.ns)
	s.Wait()
}
