package controllers

import (
	"fmt"

	"github.com/go-logr/logr"
	pvcv1alpha1 "github.com/konveyor/volume-snapshot-mover/api/v1alpha1"
	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *DataMoverBackupReconciler) MirrorVolumeSnapshot(log logr.Logger) (bool, error) {
	// Get datamoverbackup from cluster
	// TODO: handle multiple DMBs
	dmb := pvcv1alpha1.DataMoverBackup{}
	if err := r.Get(r.Context, r.req.NamespacedName, &dmb); err != nil {
		r.Log.Error(err, "unable to fetch DataMoverBackup CR")
		return false, err
	}

	vscInCluster := snapv1.VolumeSnapshotContent{}
	if err := r.Get(r.Context, types.NamespacedName{Name: dmb.Spec.VolumeSnapshotContent.Name}, &vscInCluster); err != nil {
		r.Log.Error(err, "volumesnapshotcontent not found")
		return false, err
	}

	// define VSC to be created as clone of spec VSC
	vsc := &snapv1.VolumeSnapshotContent{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-clone", vscInCluster.Name),
			Labels: map[string]string{
				DMBLabel: dmb.Name,
			},
		},
	}

	// Create VSC in cluster
	op, err := controllerutil.CreateOrUpdate(r.Context, r.Client, vsc, func() error {

		return r.buildVolumeSnapshotContent(vsc, &dmb)
	})

	if err != nil {
		return false, err
	}

	if op == controllerutil.OperationResultCreated || op == controllerutil.OperationResultUpdated {

		r.EventRecorder.Event(vsc,
			corev1.EventTypeNormal,
			"VolumeSnapshotContentReconciled",
			fmt.Sprintf("performed %s on volumesnapshotcontent %s", op, vsc.Name),
		)
	}

	vs := &snapv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vsc.Spec.VolumeSnapshotRef.Name,
			Namespace: vsc.Spec.VolumeSnapshotRef.Namespace,
			Labels: map[string]string{
				DMBLabel: dmb.Name,
			},
		},
	}

	// Create VolumeSnapshot in the protected namespace
	op, err = controllerutil.CreateOrUpdate(r.Context, r.Client, vs, func() error {

		return r.buildVolumeSnapshot(vs, vsc)
	})
	if err != nil {
		return false, err
	}
	if op == controllerutil.OperationResultCreated || op == controllerutil.OperationResultUpdated {

		r.EventRecorder.Event(vs,
			corev1.EventTypeNormal,
			"VolumeSnapshotReconciled",
			fmt.Sprintf("performed %s on volumesnapshot %s", op, vs.Name),
		)
	}

	return true, nil
}

func (r *DataMoverBackupReconciler) buildVolumeSnapshotContent(vsc *snapv1.VolumeSnapshotContent, dmb *pvcv1alpha1.DataMoverBackup) error {
	// Get VSC that is defined in spec
	vscInCluster := snapv1.VolumeSnapshotContent{}
	if err := r.Get(r.Context, types.NamespacedName{Name: dmb.Spec.VolumeSnapshotContent.Name}, &vscInCluster); err != nil {
		r.Log.Error(err, "unable to fetch volumesnapshotcontent in cluster")
		return err
	}

	// Make a new spec that points to same snapshot handle
	newSpec := snapv1.VolumeSnapshotContentSpec{
		DeletionPolicy: vscInCluster.Spec.DeletionPolicy,
		Driver:         vscInCluster.Spec.Driver,
		VolumeSnapshotRef: corev1.ObjectReference{
			APIVersion: vscInCluster.Spec.VolumeSnapshotRef.APIVersion,
			Kind:       vscInCluster.Spec.VolumeSnapshotRef.Kind,
			Namespace:  dmb.Spec.ProtectedNamespace,
			Name:       fmt.Sprintf("%s-volumesnapshot", vscInCluster.Name),
		},
		VolumeSnapshotClassName: vscInCluster.Spec.VolumeSnapshotClassName,
		Source: snapv1.VolumeSnapshotContentSource{
			SnapshotHandle: vscInCluster.Status.SnapshotHandle,
		},
	}

	vsc.Spec = newSpec
	return nil
}

func (r *DataMoverBackupReconciler) buildVolumeSnapshot(vs *snapv1.VolumeSnapshot, vsc *snapv1.VolumeSnapshotContent) error {
	// Get VS that is defined in spec
	vsSpec := snapv1.VolumeSnapshotSpec{
		Source: snapv1.VolumeSnapshotSource{
			VolumeSnapshotContentName: &vsc.Name,
		},
	}

	vs.Spec = vsSpec
	return nil
}
