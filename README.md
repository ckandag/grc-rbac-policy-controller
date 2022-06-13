[comment]: # ( Copyright Contributors to the Open Cluster Management project )

# Governance RBAC Policy Controller 

## Description

This operator runs on the Hub and  watches for changes to the  `Policies` resources in any cluster naemspaces to trigger a reconcile. On each reconcile, it

- It checks to see if its a ConfiurationPolicy
- Checks to see if it has an annotation to indicate it needs to be  processed for finer-grained RBAC 
    ```annotation: policy.open-cluster-management.io/process-rbac```
- Processes the RoleBinding objects in the configurationPolicy
    - Identify the Namespace
    - Identify the Subject ( User or Usergroup)
    - Identify the Role being assigned ( should process only if its   either admin, view, edit )
- Find the target clusters of the policy through   placement decisions
- Build the rbac-rules json and call the OPA service to pass the rbac information to OPA
-Save the rbac-rules json  applied to the OPA in the status of the policy object


Policy Watch Actions
- On Policy object Create action, patch the additional rbac rules to OPA service, save the rbac-json rules in object status
- On Policy object delete action, delete the rbac rules OPA service
- On Policy object update action, compare the old json rules and new Rolesbindings and patch or delete rbac rules in OPA


##Example 

Conside the Rolebinding object in a ConfigurationPolicy deployed to managed-cluster "mc-test-1"

```
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
    name: App0-Dev-Grp-binding1
    namespace: ns0
subjects:
    - kind: User
    apiGroup: rbac.authorization.k8s.io
    name: user1
roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: view
```

Rbac-Rules json passed OPA service api would be 

{
    "user" : "user1",
    "managedcluster" : "mc-test-1",
    "namespaces" : [ "ns0"],
    "role" : "view"
}



## Geting started

Go to the
[Contributing guide](https://github.com/open-cluster-management-io/community/blob/main/sig-policy/contribution-guidelines.md)
to learn how to get involved.

Check the [Security guide](SECURITY.md) if you need to report a security issue.

### Build and deploy locally
You will need [kind](https://kind.sigs.k8s.io/docs/user/quick-start/) installed.

```bash
make kind-bootstrap-cluster-dev
make build-images
make kind-deploy-controller-dev
```
### Running tests
```
make test-dependencies
make test

make e2e-dependencies
make e2e-test
```

### Clean up
```
make kind-delete-cluster
```

## References

- The `governance-policy-rbac-sync` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).

<!---
Date: April/29/2022
-->
