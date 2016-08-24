package policybased

import (
	"errors"
	"fmt"

	kapi "k8s.io/kubernetes/pkg/api"
	kapierrors "k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/rest"
	"k8s.io/kubernetes/pkg/api/unversioned"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"

	oapi "github.com/openshift/origin/pkg/api"
	authorizationapi "github.com/openshift/origin/pkg/authorization/api"
	authorizationinterfaces "github.com/openshift/origin/pkg/authorization/interfaces"
	policyregistry "github.com/openshift/origin/pkg/authorization/registry/policy"
	roleregistry "github.com/openshift/origin/pkg/authorization/registry/role"
	"github.com/openshift/origin/pkg/authorization/rulevalidation"
)

// TODO sort out resourceVersions.  Perhaps a hash of the object contents?

type VirtualStorage struct {
	PolicyStorage policyregistry.Registry

	RuleResolver   rulevalidation.AuthorizationRuleResolver
	CreateStrategy rest.RESTCreateStrategy
	UpdateStrategy rest.RESTUpdateStrategy
}

// NewVirtualStorage creates a new REST for policies.
func NewVirtualStorage(policyStorage policyregistry.Registry, ruleResolver rulevalidation.AuthorizationRuleResolver) roleregistry.Storage {
	return &VirtualStorage{policyStorage, ruleResolver, roleregistry.LocalStrategy, roleregistry.LocalStrategy}
}

func (m *VirtualStorage) New() runtime.Object {
	return &authorizationapi.Role{}
}
func (m *VirtualStorage) NewList() runtime.Object {
	return &authorizationapi.RoleList{}
}

func (m *VirtualStorage) List(ctx kapi.Context, options *kapi.ListOptions) (runtime.Object, error) {
	policyList, err := m.PolicyStorage.ListPolicies(ctx, options)
	if err != nil {
		return nil, err
	}

	labelSelector, fieldSelector := oapi.ListOptionsToSelectors(options)

	roleList := &authorizationapi.RoleList{}
	for _, policy := range policyList.Items {
		for _, role := range policy.Roles {
			if labelSelector.Matches(labels.Set(role.Labels)) &&
				fieldSelector.Matches(authorizationapi.RoleToSelectableFields(role)) {
				roleList.Items = append(roleList.Items, *role)
			}
		}
	}

	return roleList, nil
}

func (m *VirtualStorage) Get(ctx kapi.Context, name string) (runtime.Object, error) {
	policy, err := m.PolicyStorage.GetPolicy(ctx, authorizationapi.PolicyName)
	if kapierrors.IsNotFound(err) {
		return nil, kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
	}
	if err != nil {
		return nil, err
	}

	role, exists := policy.Roles[name]
	if !exists {
		return nil, kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
	}

	return role, nil
}

// Delete(ctx api.Context, name string) (runtime.Object, error)
func (m *VirtualStorage) Delete(ctx kapi.Context, name string, options *kapi.DeleteOptions) (runtime.Object, error) {
	if err := kclient.RetryOnConflict(kclient.DefaultRetry, func() error {
		policy, err := m.PolicyStorage.GetPolicy(ctx, authorizationapi.PolicyName)
		if kapierrors.IsNotFound(err) {
			return kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
		}
		if err != nil {
			return err
		}

		if _, exists := policy.Roles[name]; !exists {
			return kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
		}

		delete(policy.Roles, name)
		policy.LastModified = unversioned.Now()

		return m.PolicyStorage.UpdatePolicy(ctx, policy)
	}); err != nil {
		return nil, err
	}

	return &unversioned.Status{Status: unversioned.StatusSuccess}, nil
}

func (m *VirtualStorage) Create(ctx kapi.Context, obj runtime.Object) (runtime.Object, error) {
	return m.createRole(ctx, obj, false)
}

func (m *VirtualStorage) CreateRoleWithEscalation(ctx kapi.Context, obj *authorizationapi.Role) (*authorizationapi.Role, error) {
	return m.createRole(ctx, obj, true)
}

func (m *VirtualStorage) createRole(ctx kapi.Context, obj runtime.Object, allowEscalation bool) (*authorizationapi.Role, error) {
	// Copy object before passing to BeforeCreate, since it mutates
	objCopy, err := kapi.Scheme.DeepCopy(obj)
	if err != nil {
		return nil, err
	}
	obj = objCopy.(runtime.Object)

	if err := rest.BeforeCreate(m.CreateStrategy, ctx, obj); err != nil {
		return nil, err
	}

	role := obj.(*authorizationapi.Role)
	if !allowEscalation {
		if err := rulevalidation.ConfirmNoEscalation(ctx, m.RuleResolver, authorizationinterfaces.NewLocalRoleAdapter(role)); err != nil {
			return nil, err
		}
	}

	if err := kclient.RetryOnConflict(kclient.DefaultRetry, func() error {
		policy, err := m.EnsurePolicy(ctx)
		if err != nil {
			return err
		}
		if _, exists := policy.Roles[role.Name]; exists {
			return kapierrors.NewAlreadyExists(authorizationapi.Resource("role"), role.Name)
		}

		role.ResourceVersion = policy.ResourceVersion
		policy.Roles[role.Name] = role
		policy.LastModified = unversioned.Now()

		return m.PolicyStorage.UpdatePolicy(ctx, policy)
	}); err != nil {
		return nil, err
	}

	return role, nil
}

func (m *VirtualStorage) Update(ctx kapi.Context, obj runtime.Object) (runtime.Object, bool, error) {
	return m.updateRole(ctx, obj, false)
}
func (m *VirtualStorage) UpdateRoleWithEscalation(ctx kapi.Context, obj *authorizationapi.Role) (*authorizationapi.Role, bool, error) {
	return m.updateRole(ctx, obj, true)
}

func (m *VirtualStorage) updateRole(ctx kapi.Context, originalObj runtime.Object, allowEscalation bool) (*authorizationapi.Role, bool, error) {
	var updatedRole *authorizationapi.Role
	var roleConflicted = false

	var name = ""
	if r, ok := originalObj.(*authorizationapi.Role); !ok {
		return nil, false, kapierrors.NewBadRequest(fmt.Sprintf("obj is not a role: %#v", originalObj))
	} else {
		name = r.Name
	}

	// Retry if the policy update hits a conflict
	if err := kclient.RetryOnConflict(kclient.DefaultRetry, func() error {
		policy, err := m.PolicyStorage.GetPolicy(ctx, authorizationapi.PolicyName)
		if kapierrors.IsNotFound(err) {
			return kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
		}
		if err != nil {
			return err
		}

		oldRole, exists := policy.Roles[name]
		if !exists {
			return kapierrors.NewNotFound(authorizationapi.Resource("role"), name)
		}

		obj, err := kapi.Scheme.Copy(originalObj)
		if err != nil {
			return err
		}

		role, ok := obj.(*authorizationapi.Role)
		if !ok {
			return kapierrors.NewBadRequest(fmt.Sprintf("obj is not a role: %#v", obj))
		}

		if len(role.ResourceVersion) == 0 && m.UpdateStrategy.AllowUnconditionalUpdate() {
			role.ResourceVersion = oldRole.ResourceVersion
		}

		if err := rest.BeforeUpdate(m.UpdateStrategy, ctx, obj, oldRole); err != nil {
			return err
		}

		if !allowEscalation {
			if err := rulevalidation.ConfirmNoEscalation(ctx, m.RuleResolver, authorizationinterfaces.NewLocalRoleAdapter(role)); err != nil {
				return err
			}
		}

		// conflict detection
		if role.ResourceVersion != oldRole.ResourceVersion {
			// mark as a conflict err, but return an untyped error to escape the retry
			roleConflicted = true
			return errors.New("the object has been modified; please apply your changes to the latest version and try again")
		}
		// non-mutating change
		if kapi.Semantic.DeepEqual(oldRole, role) {
			updatedRole = role
			return nil
		}

		role.ResourceVersion = policy.ResourceVersion
		policy.Roles[role.Name] = role
		policy.LastModified = unversioned.Now()

		if err := m.PolicyStorage.UpdatePolicy(ctx, policy); err != nil {
			return err
		}
		updatedRole = role
		return nil
	}); err != nil {
		if roleConflicted {
			// construct the typed conflict error
			return nil, false, kapierrors.NewConflict(authorizationapi.Resource("name"), name, err)
		}
		return nil, false, err
	}

	return updatedRole, false, nil
}

// EnsurePolicy returns the policy object for the specified namespace.  If one does not exist, it is created for you.  Permission to
// create, update, or delete roles in a namespace implies the ability to create a Policy object itself.
func (m *VirtualStorage) EnsurePolicy(ctx kapi.Context) (*authorizationapi.Policy, error) {
	policy, err := m.PolicyStorage.GetPolicy(ctx, authorizationapi.PolicyName)
	if err != nil {
		if !kapierrors.IsNotFound(err) {
			return nil, err
		}

		// if we have no policy, go ahead and make one.  creating one here collapses code paths below.  We only take this hit once
		policy = NewEmptyPolicy(kapi.NamespaceValue(ctx))
		if err := m.PolicyStorage.CreatePolicy(ctx, policy); err != nil {
			// Tolerate the policy having been created in the meantime
			if !kapierrors.IsAlreadyExists(err) {
				return nil, err
			}
		}

		policy, err = m.PolicyStorage.GetPolicy(ctx, authorizationapi.PolicyName)
		if err != nil {
			return nil, err
		}

	}

	if policy.Roles == nil {
		policy.Roles = make(map[string]*authorizationapi.Role)
	}

	return policy, nil
}

func NewEmptyPolicy(namespace string) *authorizationapi.Policy {
	policy := &authorizationapi.Policy{}
	policy.Name = authorizationapi.PolicyName
	policy.Namespace = namespace
	policy.CreationTimestamp = unversioned.Now()
	policy.LastModified = unversioned.Now()
	policy.Roles = make(map[string]*authorizationapi.Role)

	return policy
}
