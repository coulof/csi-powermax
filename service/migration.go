/*
Copyright © 2021 Dell Inc. or its subsidiaries. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package service

import (
	"fmt"
	"path"
	"strconv"
	"time"

	csiext "github.com/dell/dell-csi-extensions/migration"
	types "github.com/dell/gopowermax/v2/types/v100"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *service) VolumeMigrate(ctx context.Context, req *csiext.VolumeMigrateRequest) (*csiext.VolumeMigrateResponse, error) {

	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	volID := req.GetVolumeHandle()
	_, symID, _, _, _, err := s.parseCsiID(volID)
	if err != nil {
		log.Errorf("Invalid volumeid: %s", volID)
		return nil, status.Errorf(codes.InvalidArgument, "Invalid volume id: %s", volID)
	}

	pmaxClient, err := s.GetPowerMaxClient(symID)
	if err != nil {
		log.Error(err.Error())
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Requires probe
	if err := s.requireProbe(ctx, pmaxClient); err != nil {
		return nil, err
	}

	symID, devID, vol, err := s.GetVolumeByID(ctx, volID, pmaxClient)
	if err != nil {
		log.Errorf("GetVolumeByID failed with (%s) for devID (%s)", err.Error(), devID)
		return nil, err
	}
	// Get the parameters

	params := req.GetScParameters()
	sourceScParams := req.GetScSourceParameters()

	applicationPrefix := ""
	if params[ApplicationPrefixParam] != "" {
		applicationPrefix = params[ApplicationPrefixParam]
	}

	storagePoolID := params[StoragePoolParam]
	err = s.validateStoragePoolID(ctx, symID, storagePoolID, pmaxClient)
	if err != nil {
		log.Error(err.Error())
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// SLO is optional
	serviceLevel := "Optimized"
	if params[ServiceLevelParam] != "" {
		serviceLevel = params[ServiceLevelParam]
		found := false
		for _, val := range validSLO {
			if serviceLevel == val {
				found = true
			}
		}
		if !found {
			log.Error("An invalid Service Level parameter was specified")
			return nil, status.Errorf(codes.InvalidArgument, "An invalid Service Level parameter was specified")
		}
	}
	storageGroupName := ""
	if params[StorageGroupParam] != "" {
		storageGroupName = params[StorageGroupParam]
	}

	migrateType := req.GetType()
	fields := log.Fields{
		"RequestID":   reqID,
		"SymmetrixID": symID,
		"RemoteSymID": sourceScParams[path.Join(s.opts.ReplicationPrefix, RemoteSymIDParam)],
		"MigrateType": migrateType,
		"VolID":       devID,
		"Namespace":   params[CSIPVCNamespace],
	}
	log.WithFields(fields).Info("Executing VolumeMigrate with following fields")

	var migrationFunc func(context.Context, map[string]string, map[string]string, string, string, string, string, string, *service, *types.Volume) error

	switch migrateType {
	case csiext.MigrateTypes_UNKNOWN_MIGRATE:
		return nil, status.Errorf(codes.Unknown, "Unknown Migration Type")
	case csiext.MigrateTypes_NON_REPL_TO_REPL:
		migrationFunc = nonReplToRepl
	case csiext.MigrateTypes_REPL_TO_NON_REPL:
		migrationFunc = replToNonRepl
	case csiext.MigrateTypes_VERSION_UPGRADE:
		migrationFunc = versionUpgrade

	}
	if err := migrationFunc(ctx, params, sourceScParams, storageGroupName, applicationPrefix, serviceLevel, storagePoolID, symID, s, vol); err != nil {
		return nil, err
	}

	attributes := map[string]string{
		ServiceLevelParam: serviceLevel,
		StoragePoolParam:  storagePoolID,
		path.Join(s.opts.ReplicationContextPrefix, SymmetrixIDParam): symID,
		CapacityGB:   fmt.Sprintf("%.2f", vol.CapacityGB),
		StorageGroup: storageGroupName,
		//Format the time output
		"MigrationTime": time.Now().Format("20060102150405"),
	}
	volume := new(csiext.Volume)
	volume.VolumeId = volID
	volume.FsType = params[FsTypeParam]
	volume.VolumeContext = attributes
	csiVol := s.getCSIVolume(vol)
	volume.CapacityBytes = csiVol.CapacityBytes
	csiResp := &csiext.VolumeMigrateResponse{
		MigratedVolume: volume,
	}

	return csiResp, nil
}

func nonReplToRepl(ctx context.Context, params map[string]string, sourceScParams map[string]string, storageGroupName, applicationPrefix, serviceLevel, storagePoolID, symID string, s *service, vol *types.Volume) error {
	var replicationEnabled string
	var remoteSymID string
	var localRDFGrpNo string
	var remoteRDFGrpNo string
	var repMode string
	var reqID string
	var namespace string

	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	pmaxClient, err := s.GetPowerMaxClient(symID)
	if err != nil {
		log.Error(err.Error())
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if params[path.Join(s.opts.ReplicationPrefix, RepEnabledParam)] == "true" {
		replicationEnabled = params[path.Join(s.opts.ReplicationPrefix, RepEnabledParam)]
		// remote symmetrix ID and rdf group name are mandatory params when replication is enabled
		remoteSymID = params[path.Join(s.opts.ReplicationPrefix, RemoteSymIDParam)]
		localRDFGrpNo = params[path.Join(s.opts.ReplicationPrefix, LocalRDFGroupParam)]
		remoteRDFGrpNo = params[path.Join(s.opts.ReplicationPrefix, RemoteRDFGroupParam)]
		repMode = params[path.Join(s.opts.ReplicationPrefix, ReplicationModeParam)]
		namespace = params[CSIPVCNamespace]
		if localRDFGrpNo == "" && remoteRDFGrpNo == "" {
			localRDFGrpNo, remoteRDFGrpNo, err = s.GetOrCreateRDFGroup(ctx, symID, remoteSymID, repMode, namespace, pmaxClient)
			if err != nil {
				return status.Errorf(codes.NotFound, "Received error get/create RDFG, err: %s", err.Error())
			}
			log.Debugf("found pre existing group for given array pair and RDF mode: local(%s), remote(%s)", localRDFGrpNo, remoteRDFGrpNo)
		}
		if repMode == Metro {
			log.Errorf("Unsupported Replication Mode: (%s)", repMode)
			return status.Errorf(codes.InvalidArgument, "Unsupported Replication Mode: (%s)", repMode)
		}
		if repMode != Async && repMode != Sync {
			log.Errorf("Unsupported Replication Mode: (%s)", repMode)
			return status.Errorf(codes.InvalidArgument, "Unsupported Replication Mode: (%s)", repMode)
		}
	}
	if storageGroupName == "" {
		if applicationPrefix == "" {
			storageGroupName = fmt.Sprintf("%s-%s-%s-%s-SG", CSIPrefix, s.getClusterPrefix(),
				serviceLevel, storagePoolID)
		} else {
			storageGroupName = fmt.Sprintf("%s-%s-%s-%s-%s-SG", CSIPrefix, s.getClusterPrefix(),
				applicationPrefix, serviceLevel, storagePoolID)
		}
	}
	// localProtectionGroupID refers to name of Storage Group which has protected local volumes
	// remoteProtectionGroupID refers to name of Storage Group which has protected remote volumes
	var localProtectionGroupID string
	var remoteProtectionGroupID string
	if replicationEnabled == "true" {
		localProtectionGroupID = buildProtectionGroupID(namespace, localRDFGrpNo, repMode)
		remoteProtectionGroupID = buildProtectionGroupID(namespace, remoteRDFGrpNo, repMode)
	}
	if replicationEnabled == "true" {
		isSGUnprotected := false
		if replicationEnabled == "true" {
			sg, err := s.getOrCreateProtectedStorageGroup(ctx, symID, localProtectionGroupID, namespace, localRDFGrpNo, repMode, reqID, pmaxClient)
			if err != nil {
				return status.Errorf(codes.Internal, "Error in getOrCreateProtectedStorageGroup: (%s)", err.Error())
			}
			if sg != nil && sg.Rdf == true {
				// Check the direction of SG
				// Creation of replicated volume is allowed in an SG of type R1
				err := s.VerifyProtectedGroupDirection(ctx, symID, localProtectionGroupID, localRDFGrpNo, pmaxClient)
				if err != nil {
					return err
				}
			} else {
				isSGUnprotected = true
			}
		}
		log.Debugf("RDF: Found Rdf enabled")
		// remote storage group name is kept same as local storage group name
		// Check if volume is already added in SG, else add it
		log.Debug("StorageGroupName", storageGroupName, "localSGID", localProtectionGroupID, "remoteSGID", remoteProtectionGroupID)
		sg, err := pmaxClient.GetStorageGroup(ctx, symID, storageGroupName)
		if err != nil || sg == nil {
			log.Debug(fmt.Sprintf("Unable to find storage group: %s", storageGroupName))
			thick := params[ThickVolumesParam]
			_, err := pmaxClient.CreateStorageGroup(ctx, symID, storageGroupName, storagePoolID,
				serviceLevel, thick == "true", nil)
			if err != nil {
				log.Error("Error creating storage group: " + err.Error())
				return status.Errorf(codes.Internal, "Error creating storage group: %s", err.Error())
			}
			log.Debug("We created SG")
		} else {
			log.Debug("SG was found")
		}
		protectedSGID := s.GetProtectedStorageGroupID(vol.StorageGroupIDList, localRDFGrpNo+"-"+repMode)
		if protectedSGID == "" {
			// Volume is not present in Protected Storage Group, Add
			log.Info("ProtectedSG not found. Trying to create...")
			err := pmaxClient.AddVolumesToProtectedStorageGroup(ctx, symID, localProtectionGroupID, remoteSymID, remoteProtectionGroupID, true, vol.VolumeID)
			if err != nil {
				log.Error(fmt.Sprintf("Could not add volume to protected SG: %s: %s", localProtectionGroupID, err.Error()))
				return status.Errorf(codes.Internal, "Could not add volume to protected SG: %s: %s", localProtectionGroupID, err.Error())
			}
		}

		log.Info("Protected SG was created")
		if isSGUnprotected {
			// If the required SG is still unprotected, protect the local SG with RDF info
			// If valid RDF group is supplied this will create a remote SG, a RDF pair and add the vol in respective SG created
			// Remote storage group name is kept same as local storage group name
			err := s.ProtectStorageGroup(ctx, symID, remoteSymID, localProtectionGroupID, remoteProtectionGroupID, "", localRDFGrpNo, repMode, vol.VolumeID, reqID, false, pmaxClient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func replToNonRepl(ctx context.Context, params map[string]string, sourceScParams map[string]string, storageGroupName, applicationPrefix, serviceLevel, storagePoolID, symID string, s *service, vol *types.Volume) error {
	pmaxClient, err := s.GetPowerMaxClient(symID)
	if err != nil {
		log.Error(err.Error())
		return status.Error(codes.InvalidArgument, err.Error())
	}

	remoteSymID := sourceScParams[path.Join(s.opts.ReplicationPrefix, RemoteSymIDParam)]
	localRDFGrpNo := sourceScParams[path.Join(s.opts.ReplicationPrefix, LocalRDFGroupParam)]
	remoteRDFGrpNo := sourceScParams[path.Join(s.opts.ReplicationPrefix, RemoteRDFGroupParam)]
	repMode := sourceScParams[path.Join(s.opts.ReplicationPrefix, ReplicationModeParam)]
	if localRDFGrpNo == "" {
		localRDFGrpNo = strconv.Itoa(vol.RDFGroupIDList[0].RDFGroupNumber)
		rdfInfo, err := pmaxClient.GetRDFGroupByID(ctx, symID, localRDFGrpNo)
		if err != nil {
			log.Error(fmt.Sprintf("Could not get remote rdfG for %s: %s:", localRDFGrpNo, err.Error()))
			return status.Errorf(codes.Internal, fmt.Sprintf("Could not get remote rdfG for %s: %s:", localRDFGrpNo, err.Error()))
		}
		remoteRDFGrpNo = strconv.Itoa(rdfInfo.RemoteRdfgNumber)
	}
	sgID := buildProtectionGroupID(params[CSIPVCNamespace], localRDFGrpNo, repMode)
	remoteSGID := buildProtectionGroupID(params[CSIPVCNamespace], remoteRDFGrpNo, repMode)

	_, err = pmaxClient.RemoveVolumesFromProtectedStorageGroup(ctx, symID, sgID, remoteSymID, remoteSGID, true, vol.VolumeID)
	if err != nil {
		log.Error(fmt.Sprintf("Could not remove volume from protected SG: %s: %s", sgID, err.Error()))
		return status.Errorf(codes.Internal, "Could not remove volume from protected SG: %s: %s", sgID, err.Error())
	}

	return nil
}

func versionUpgrade(ctx context.Context, params map[string]string, sourceScParams map[string]string, storageGroupName, applicationPrefix, serviceLevel, storagePoolID, symID string, s *service, vol *types.Volume) error {
	return status.Error(codes.Unimplemented, "Unimplemented")
}
