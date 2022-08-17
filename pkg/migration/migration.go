package migration

import (
	"context"
	"fmt"

	pmax "github.com/dell/gopowermax/v2"
	types "github.com/dell/gopowermax/v2/types/v100"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//NodePairs contains -  0002D (local): 00031 (remote)
var NodePairs map[string]string

// SGToRemoteVols contains -  csi-ABC-local-sg : [00031, ...]
var SGToRemoteVols map[string][]string

const (
	CsiNoSrpSGPrefix = "csi-no-srp-sg-"
	CsiVolumePrefix  = "csi-"
)

func getorCreateSGMigration(ctx context.Context, symID, remoteSymID, storageGroupID string, pmaxClient pmax.Pmax) (*types.MigrationSession, error) {
	migrationSG, err := pmaxClient.GetStorageGroupMigration(ctx, symID, remoteSymID, storageGroupID)
	if err != nil || migrationSG == nil {
		// not found
		migrationSG, err = pmaxClient.CreateSGMigration(ctx, symID, remoteSymID, storageGroupID)
		if err != nil {
			return nil, err
		}
	}
	return migrationSG, nil
}

func ListContains(a []string, x string) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func StorageGroupMigration(ctx context.Context, symID, remoteSymID, clusterPrefix string, pmaxClient pmax.Pmax) (bool, error) {
	// for no-srp-sg-<CLUSTER>
	localSgList, err := pmaxClient.GetStorageGroupIDList(ctx, symID, CsiNoSrpSGPrefix+clusterPrefix, true)
	if err != nil {
		return false, err
	}
	for _, storageGroupID := range localSgList.StorageGroupIDs {
		// send migrate call to remote side
		// Masking view ??
		// Doubt --> from the API it should migrate the SG along with the Volume. What is the behavior here ??
		migrationSG, err := getorCreateSGMigration(ctx, symID, remoteSymID, storageGroupID, pmaxClient)
		if err != nil {
			log.Errorf("Failed to create array migration environment for target array (%s) - Error (%s)", remoteSymID, err.Error())
			return false, status.Errorf(codes.Internal, "to create array migration environment for target array(%s) - Error (%s)", remoteSymID, err.Error())
		}
		// fill in NodePairs
		if NodePairs == nil {
			NodePairs = make(map[string]string)
		}
		for _, pair := range migrationSG.DevicePairs {
			NodePairs[pair.SrcVolumeName] = pair.TgtVolumeName
		}
	}

	// for all the SG for this cluster on local sym ID
	localSgList, err = pmaxClient.GetStorageGroupIDList(ctx, symID, CsiVolumePrefix+clusterPrefix, true)
	if err != nil {
		return false, err
	}
	// for all the SG for this cluster on remote sym ID
	remoteSgList, err := pmaxClient.GetStorageGroupIDList(ctx, remoteSymID, CsiVolumePrefix+clusterPrefix, true)
	if err != nil {
		return false, err
	}

	for _, localSGID := range localSgList.StorageGroupIDs {
		//check if SG is present on remote side or not
		output := ListContains(remoteSgList.StorageGroupIDs, localSGID)
		if !output {

			// get local SG Params
			sg, err := pmaxClient.GetStorageGroup(ctx, symID, localSGID)
			// create SG on remote SG
			_, err = pmaxClient.CreateStorageGroup(ctx, remoteSymID, localSGID, sg.SRP,
				sg.SLO, true)
			if err != nil {
				log.Error("Error creating storage group on remote array: " + err.Error())
				return false, status.Errorf(codes.Internal, "Error creating storage group on remote array: %s", err.Error())
			}

			volumeIDList, err := pmaxClient.GetVolumesInStorageGroupIterator(ctx, symID, localSGID)
			if err != nil {
				log.Error("Error getting  storage group volume list: " + err.Error())
				return false, status.Errorf(codes.Internal, "Error getting storage group volume list: %s", err.Error())
			}
			var remoteVolIDs []string
			for _, vol := range volumeIDList.ResultList.VolumeList {
				remoteVolIDs = append(remoteVolIDs, NodePairs[vol.VolumeIDs])
			}
			// initialize
			if SGToRemoteVols == nil {
				SGToRemoteVols = make(map[string][]string)
			}
			SGToRemoteVols[localSGID] = remoteVolIDs
		}

	}
	return true, nil
}

func StorageGroupCommit(ctx context.Context, symID, action string, pmaxClient pmax.Pmax) (bool, error) {
	// for all the SG in saved local SG
	for sgID, _ := range SGToRemoteVols {
		_, err := pmaxClient.ModifyMigrationSession(ctx, symID, action, sgID)
		if err != nil {
			return false, status.Errorf(codes.Internal, "error modifying Migration session for SG %s on sym %s: %s", sgID, symID, err.Error())
		}
	}
	return true, nil
}

func AddVolumesToRemoteSG(ctx context.Context, remoteSymID string, pmaxClient pmax.Pmax) (bool, error) {
	// add all the volumes in SGtoRemoteVols
	for storageGroup, remoteVols := range SGToRemoteVols {
		err := pmaxClient.AddVolumesToStorageGroupS(ctx, remoteSymID, storageGroup, true, remoteVols...)
		if err != nil {
			log.Error(fmt.Sprintf("Could not add volume in SG on R2: %s: %s", remoteSymID, err.Error()))
			return false, status.Errorf(codes.Internal, "Could not add volume in SG on R2: %s: %s", remoteSymID, err.Error())
		}
	}
	return true, nil
}

func GetOrCreateMigrationEnvironment(ctx context.Context, localSymID, remoteSymID string, pmaxClient pmax.Pmax) (*types.MigrationEnv, error) {
	var migrationEnv *types.MigrationEnv
	migrationEnv, err := pmaxClient.GetMigrationEnvironment(ctx, localSymID, remoteSymID)
	if err != nil || migrationEnv == nil {
		migrationEnv, err = pmaxClient.CreateMigrationEnvironment(ctx, localSymID, remoteSymID)
		if err != nil {
			return nil, err
		}
	}
	return migrationEnv, err
}
