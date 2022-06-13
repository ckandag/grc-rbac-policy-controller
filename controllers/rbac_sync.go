// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"encoding/json"

	rbacV1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	configpoliciesv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName string = "policy-rbac-sync"
	policyFmtStr   string = "policy: %s/%s"
)

var log = ctrl.Log.WithName(ControllerName)

//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&policiesv1.Policy{}).
		Complete(r)
}

// blank assignment to verify that ReconcilePolicy implements reconcile.Reconciler
var _ reconcile.Reconciler = &PolicyReconciler{}

// PolicyReconciler reconciles a Policy object
type PolicyReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	Scheme   *runtime.Scheme
	Config   *rest.Config
	Recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a Policy object and makes changes based on the state read
// and what is in the Policy.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *PolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling the Policy")

	// Fetch the Policy instance
	instance := &policiesv1.Policy{}

	err := r.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Policy not found, may have been deleted, reconciliation completed")

			return reconcile.Result{}, nil
		}

		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Failed to get the policy, will requeue the request")

		return reconcile.Result{}, err
	}

	// //check for annotation and process policy
	// annotations := instance.GetAnnotations()
	// if process_rbac, ok := annotations["policy.open-cluster-management.io/process-rbac"]; ok {
	// 	if boolProcessRbac, err := strconv.ParseBool(process_rbac); err == nil && boolProcessRbac {
	// 		log.Info("Detected the process-rbac annotation. Will process rbac.")
	// 		processRbacInPolicy(instance)
	// 	}
	// }

	for _, policyT := range instance.Spec.PolicyTemplates {
		if isConfigurationPolicy(policyT) {
			log.Info("Is Config Policy.")

			var configPolicy configpoliciesv1.ConfigurationPolicy //.map[string]interface{}
			_ = json.Unmarshal(policyT.ObjectDefinition.Raw, &configPolicy)

			for _, objectT := range configPolicy.Spec.ObjectTemplates {
				log.Info("Is Config Policy.")
				var rolebinding rbacV1.RoleBinding
				_ = json.Unmarshal(objectT.ObjectDefinition.Raw, &rolebinding)
				log.Info("Role Ref Name is " + rolebinding.RoleRef.Name)
			}

		}
	}
	reqLogger.Info("Completed the reconciliation")

	return ctrl.Result{}, nil
}

// func processRbacInPolicy(instance *policiesv1.Policy) error {
// 	log.Info("processRbacInPolicy.")
// 	return nil
// }

func isConfigurationPolicy(policyT *policiesv1.PolicyTemplate) bool {
	// check if it is a configuration policy first
	var jsonDef map[string]interface{}
	_ = json.Unmarshal(policyT.ObjectDefinition.Raw, &jsonDef)

	return jsonDef != nil && jsonDef["kind"] == "ConfigurationPolicy"
}
