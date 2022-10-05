// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"

	"bytes"
	"io/ioutil"
	"net/http"
	"time"

	rbacV1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	configpoliciesv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"

	appsv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName string = "policy-rbac-sync"
	policyFmtStr   string = "policy: %s/%s"
)

type aclentry struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Rules     []aclrule `json:"acl"`
}

type policyrb struct {
	PolicyRoleBinding []aclentry `json:"policyrolebindings"`
}

type aclrule struct {
	Subject        string `json:"user"`
	ManagedCluster string `json:"managedcluster"`
	Namespace      string `json:"namespace"`
	Role           string `json:"role"`
}

type patch struct {
	Op    string   `json:"op"`
	Path  string   `json:"path"`
	Value aclentry `json:"value"`
}

var log = ctrl.Log.WithName(ControllerName)

//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
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
	reqLogger.Info("Reconciling the Policy", "entire request", request)

	// Fetch the Policy instance
	instance := &policiesv1.Policy{}

	err := r.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Policy not found, may have been deleted, reconciliation completed")

			//remove any contents for it from OPA
			var acl aclentry
			acl.Namespace = request.Namespace + request.Name
			acl.Name = request.Name
			patchACL(acl, false)

			return reconcile.Result{}, nil
		}

		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Failed to get the policy, will requeue the request")

		return reconcile.Result{}, err
	}

	// //check for annotation and process policy
	annotations := instance.GetAnnotations()
	if process_rbac, ok := annotations["policy.open-cluster-management.io/process-rbac"]; ok {
		if boolProcessRbac, err := strconv.ParseBool(process_rbac); err == nil && boolProcessRbac {
			log.Info("Detected annotation for processing rbac.")

			//find all the managedcluster this policy is placed too
			managedclusters, err1 := r.getManagedClusters(ctx, instance)
			if err1 != nil {
				reqLogger.Error(err1, "Failed to find the placements for  the policy")
				return reconcile.Result{}, err1
			}
			log.Info("printing data", " AllManagedClusters :", managedclusters)

			for policyIndex, policyT := range instance.Spec.PolicyTemplates {
				if isConfigurationPolicy(policyT) {
					log.Info("Is Config Policy.")

					var acl aclentry
					acl.Namespace = request.Namespace
					acl.Name = request.Name

					var configPolicy configpoliciesv1.ConfigurationPolicy //.map[string]interface{}
					err2 := json.Unmarshal(policyT.ObjectDefinition.Raw, &configPolicy)
					if err2 != nil {
						reqLogger.Error(err2, "Failed to find ConfigurationPolicy in the ObjectDefinition", "policyIndex", policyIndex)
						continue
					}

					for templateIndex, objectT := range configPolicy.Spec.ObjectTemplates {
						log.Info("Is Config Policy.")
						var rolebinding rbacV1.RoleBinding
						err3 := json.Unmarshal(objectT.ObjectDefinition.Raw, &rolebinding)

						if err3 != nil || rolebinding.Kind != "RoleBinding" {
							reqLogger.Error(err3, "Failed to find RoleBinding in the ObjectTemplate", "templateIndex", templateIndex)
							continue
						}

						//subject
						subject := rolebinding.Subjects[0].Name
						//rolename
						roleName := rolebinding.RoleRef.Name
						//namespace
						roleNS := rolebinding.Namespace

						log.Info("Found RoleBinding: Subject: " + subject + " Role: " + roleName + " Namespace: " + roleNS)

						//for each placement decision, make a call to opa to update
						// or all of them as an array in one call ?
						for _, mc := range managedclusters {
							//make a call to OPA to update db
							acl.Rules = append(acl.Rules, aclrule{ManagedCluster: mc, Subject: subject, Role: roleName, Namespace: roleNS})
							//patch(mc, subject, roleName, roleNS)
							//get()
						}
					}

					patchACL(acl, true)

				}
			}
		}
	}

	//two choices for update
	//either save status of last update in the policy in order to compare and update only the modified contents
	//or save the policyname + contents in OPA , so can be replaced with new easily, will require rewriting OPA rules a bit

	reqLogger.Info("Completed the reconciliation")

	return ctrl.Result{}, nil
}

func isConfigurationPolicy(policyT *policiesv1.PolicyTemplate) bool {
	// check if it is a configuration policy first
	var jsonDef map[string]interface{}
	_ = json.Unmarshal(policyT.ObjectDefinition.Raw, &jsonDef)

	return jsonDef != nil && jsonDef["kind"] == "ConfigurationPolicy"
}

func (r *PolicyReconciler) getManagedClusters(ctx context.Context, instance *policiesv1.Policy) (managedClusters []string, err error) {
	//need to get current list of placement bindings and placement decisions for this policy
	// Get the placement binding in order to later get the placement decisions
	pbList := &policiesv1.PlacementBindingList{}

	log.Info("Getting the placement bindings namespace:" + instance.GetNamespace())
	err = r.List(ctx, pbList, &client.ListOptions{Namespace: instance.GetNamespace()})
	if err != nil {
		log.Info("error listing the placement bindings ")
		return nil, err
	}

	var allManagedClusters []string
	for _, pb := range pbList.Items {
		subjects := pb.Subjects
		for _, subject := range subjects {

			if !(subject.APIGroup == policiesv1.SchemeGroupVersion.Group &&
				subject.Kind == policiesv1.Kind &&
				subject.Name == instance.GetName()) {
				continue
			}
			log.Info("Found placementBinding ", "pb:", pb)

			//find placementDecisions
			if pb.PlacementRef.APIGroup == appsv1.SchemeGroupVersion.Group && pb.PlacementRef.Kind == "PlacementRule" {

				plr := &appsv1.PlacementRule{}

				err := r.Client.Get(ctx, types.NamespacedName{
					Namespace: instance.GetNamespace(),
					Name:      pb.PlacementRef.Name,
				}, plr)
				// no error when not found
				if err != nil {
					log.Error(
						err,
						"Failed to get the PlacementRule",
						"namespace", instance.GetNamespace(),
						"name", pb.PlacementRef.Name,
					)

					return nil, err
				}

				decisions := plr.Status.Decisions
				log.Info("List data ", "placement decisions:", decisions)
				//append to allDecisions
				for _, decision := range decisions {
					allManagedClusters = append(allManagedClusters, decision.ClusterName)
				}
				log.Info("List data ", "all manageclusters:", allManagedClusters)
			}
		}
	}

	return allManagedClusters, nil
}

func get() ([]byte, error) {

	resp, err := http.Get("https://localhost:8181/v1/data/policyrolebindings")
	if err != nil {
		log.Error(err, "get failed")
		return nil, err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "get failed")
		return nil, err
	}

	log.Info("body", "resutl", body)
	return body, nil
}

func getIndex(acl aclentry) (int, error) {
	content, err := get()
	if err != nil {
		fmt.Println("error getting content")
		return -1, err
	}
	var r policyrb
	if err := json.Unmarshal(content, &r); err != nil {
		log.Error(err, "Failed to parse json")
		return -1, err
	}

	for index, aclitem := range r.PolicyRoleBinding {
		if aclitem.Name == acl.Name && aclitem.Namespace == acl.Namespace {
			return index, nil
		}
	}
	return -1, nil
}

type Patches []patch

func patchACL(acl aclentry, add bool) {
	log.Info("printing data", " aclEntry: :", acl)

	op := "add"
	index, err := getIndex(acl)
	if err != nil {
		return
	}
	if index < 0 && !add {
		log.Info("couldn't find entry to remove")
		return
	}
	path := "-"
	if add && index >= 0 {
		op = "replace"
		path = fmt.Sprintf("/policyrolebindings[%d]/", index)
	}
	if !add {
		op = "remove"
		path = fmt.Sprintf("/policyrolebindings[%d]/", index)
	}

	var mypatch patch
	mypatch.Op = op
	mypatch.Path = path
	mypatch.Value = acl

	var patchlist = make(Patches, 1)
	patchlist[0] = mypatch
	//patchlist = append(patchlist, mypatch)
	jsonRequestBody, err := json.Marshal(patchlist)
	if err != nil {
		log.Error(err, "Failed to marshall json")
		return
	}
	log.Info("printing data", " jsonData: :", string(jsonRequestBody))

	timeout := time.Duration(100 * time.Second)
	client := &http.Client{
		Timeout: timeout,
	}
	request, err := http.NewRequest(http.MethodPatch, "https://localhost:8181/v1/data/policyrolebindings", bytes.NewBuffer(jsonRequestBody))

	request.Header.Set("Content-type", "application/json-patch+json")

	if err != nil {
		log.Error(err, "update to OPA failed")
		return
	}
	resp, err := client.Do(request)
	if err != nil {
		log.Error(err, "update to OPA failed")
		return
	}

	defer resp.Request.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "update to OPA  failed")
		return
	}

	log.Info(string(body))
}
