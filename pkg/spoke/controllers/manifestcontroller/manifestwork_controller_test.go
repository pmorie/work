package manifestcontroller

import (
	"fmt"
	"testing"
	"time"

	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/controllers"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

type testController struct {
	controller    *ManifestWorkController
	dynamicClient *fakedynamic.FakeDynamicClient
	workClient    *fakeworkclient.Clientset
	kubeClient    *fakekube.Clientset
}

func newController(work *workapiv1.ManifestWork, mapper *resource.Mapper) *testController {
	fakeWorkClient := fakeworkclient.NewSimpleClientset(work)
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(fakeWorkClient, 5*time.Minute, workinformers.WithNamespace("cluster1"))

	controller := &ManifestWorkController{
		manifestWorkClient: fakeWorkClient.WorkV1().ManifestWorks("cluster1"),
		manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
		restMapper:         mapper,
	}

	store := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore()
	store.Add(work)

	return &testController{
		controller: controller,
		workClient: fakeWorkClient,
	}
}

func (t *testController) withKubeObject(objects ...runtime.Object) *testController {
	kubeClient := fakekube.NewSimpleClientset(objects...)
	t.controller.spokeKubeclient = kubeClient
	t.kubeClient = kubeClient
	return t
}

func (t *testController) withUnstructuredObject(objects ...runtime.Object) *testController {
	scheme := runtime.NewScheme()
	dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
	t.controller.spokeDynamicClient = dynamicClient
	t.dynamicClient = dynamicClient
	return t
}

func assertCondition(t *testing.T, conditions []workapiv1.StatusCondition, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	conditionTypeFound := false
	for _, condition := range conditions {
		if condition.Type != expectedCondition {
			continue
		}
		conditionTypeFound = true
		if condition.Status != expectedStatus {
			t.Errorf("expected %s but got: %s", expectedStatus, condition.Status)
			break
		}
	}

	if !conditionTypeFound {
		t.Errorf("expected condition %s but got: %#v", expectedCondition, conditions)
	}
}

func assertManifestCondition(
	t *testing.T, conds []workapiv1.ManifestCondition, index int32, expectedCondition string, expectedStatus metav1.ConditionStatus) {
	cond := findManifestConditionByIndex(index, conds)
	if cond == nil {
		t.Errorf("expected to find the condition with index %d", index)
	}

	assertCondition(t, cond.Conditions, expectedCondition, expectedStatus)
}

type testCase struct {
	name                       string
	workManifest               []*unstructured.Unstructured
	spokeObject                []runtime.Object
	spokeDynamicObject         []runtime.Object
	expectedWorkAction         []string
	expectedKubeAction         []string
	expectedDynamicAction      []string
	expectedManifestConditions []expectedCondition
	expectedWorkConditions     []expectedCondition
}

type expectedCondition struct {
	conditionType string
	status        metav1.ConditionStatus
}

func newTestCase(name string) *testCase {
	return &testCase{
		name:                       name,
		workManifest:               []*unstructured.Unstructured{},
		spokeObject:                []runtime.Object{},
		spokeDynamicObject:         []runtime.Object{},
		expectedWorkAction:         []string{},
		expectedKubeAction:         []string{},
		expectedDynamicAction:      []string{},
		expectedManifestConditions: []expectedCondition{},
		expectedWorkConditions:     []expectedCondition{},
	}
}

func (t *testCase) withWorkManifest(objects ...*unstructured.Unstructured) *testCase {
	t.workManifest = objects
	return t
}

func (t *testCase) withSpokeObject(objects ...runtime.Object) *testCase {
	t.spokeObject = objects
	return t
}

func (t *testCase) withSpokeDynamicObject(objects ...runtime.Object) *testCase {
	t.spokeDynamicObject = objects
	return t
}

func (t *testCase) withExpectedWorkAction(actions ...string) *testCase {
	t.expectedWorkAction = actions
	return t
}

func (t *testCase) withExpectedKubeAction(actions ...string) *testCase {
	t.expectedKubeAction = actions
	return t
}

func (t *testCase) withExpectedDynamicAction(actions ...string) *testCase {
	t.expectedDynamicAction = actions
	return t
}

func (t *testCase) withExpectedManifestCondition(conds ...expectedCondition) *testCase {
	t.expectedManifestConditions = conds
	return t
}

func (t *testCase) withExpectedWorkCondition(conds ...expectedCondition) *testCase {
	t.expectedWorkConditions = conds
	return t
}

func (t *testCase) validate(
	ts *testing.T,
	dynamicClient *fakedynamic.FakeDynamicClient,
	workClient *fakeworkclient.Clientset,
	kubeClient *fakekube.Clientset) {
	workActions := workClient.Actions()
	if len(workActions) != len(t.expectedWorkAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedWorkAction), workActions)
	}
	for index := range workActions {
		spoketesting.AssertAction(ts, workActions[index], t.expectedWorkAction[index])
	}

	spokeDynamicActions := dynamicClient.Actions()
	if len(spokeDynamicActions) != len(t.expectedDynamicAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedDynamicAction), spokeDynamicActions)
	}
	for index := range spokeDynamicActions {
		spoketesting.AssertAction(ts, spokeDynamicActions[index], t.expectedDynamicAction[index])
	}
	spokeKubeActions := kubeClient.Actions()
	if len(spokeKubeActions) != len(t.expectedKubeAction) {
		ts.Errorf("Expected %d action but got %#v", len(t.expectedKubeAction), spokeKubeActions)
	}
	for index := range spokeKubeActions {
		spoketesting.AssertAction(ts, spokeKubeActions[index], t.expectedKubeAction[index])
	}

	actual, ok := workActions[len(workActions)-1].(clienttesting.UpdateActionImpl)
	if !ok {
		ts.Errorf("Expected to get update action")
	}
	actualWork := actual.Object.(*workapiv1.ManifestWork)
	for index, cond := range t.expectedManifestConditions {
		assertManifestCondition(ts, actualWork.Status.ResourceStatus.Manifests, int32(index), cond.conditionType, cond.status)
	}
	for _, cond := range t.expectedWorkConditions {
		assertCondition(ts, actualWork.Status.Conditions, cond.conditionType, cond.status)
	}
}

func newCondition(name, status, reason, message string, lastTransition *metav1.Time) workapiv1.StatusCondition {
	ret := workapiv1.StatusCondition{
		Type:    name,
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}

func newManifestCondition(ordinal int32, resource string, conds ...workapiv1.StatusCondition) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{Ordinal: ordinal, Resource: resource},
		Conditions:   conds,
	}
}

func findManifestConditionByIndex(index int32, conds []workapiv1.ManifestCondition) *workapiv1.ManifestCondition {
	// Finds the cond conds that ordinal is the same as index
	if conds == nil {
		return nil
	}
	for i, cond := range conds {
		if index == cond.ResourceMeta.Ordinal {
			return &conds[i]
		}
	}

	return nil
}

// TestSync test cases when running sync
func TestSync(t *testing.T) {
	cases := []*testCase{
		newTestCase("create single resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single deployment resource").
			withWorkManifest(spoketesting.NewUnstructured("apps/v1", "Deployment", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("create single unstructured resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "NewObject", "ns1", "test")).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("update single unstructured resource").
			withWorkManifest(spoketesting.NewUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}})).
			withSpokeDynamicObject(spoketesting.NewUnstructuredWithContent("v1", "NewObject", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}})).
			withExpectedWorkAction("get", "update").
			withExpectedDynamicAction("get", "update").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
		newTestCase("multiple create&update resource").
			withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"), spoketesting.NewUnstructured("v1", "Secret", "ns2", "test")).
			withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
			withExpectedWorkAction("get", "update").
			withExpectedKubeAction("get", "delete", "create", "get", "create").
			withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}).
			withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionTrue}),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work, workKey := spoketesting.NewManifestWork(0, c.workManifest...)
			work.Finalizers = []string{controllers.ManifestWorkFinalizer}
			controller := newController(work, spoketesting.NewFakeRestMapper()).
				withKubeObject(c.spokeObject...).
				withUnstructuredObject(c.spokeDynamicObject...)
			syncContext := spoketesting.NewFakeSyncContext(t, workKey)
			err := controller.controller.sync(nil, syncContext)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			c.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
		})
	}
}

// Test applying resource failed
func TestFailedToApplyResource(t *testing.T) {
	tc := newTestCase("multiple create&update resource").
		withWorkManifest(spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"), spoketesting.NewUnstructured("v1", "Secret", "ns2", "test")).
		withSpokeObject(spoketesting.NewSecret("test", "ns1", "value2")).
		withExpectedWorkAction("get", "update").
		withExpectedKubeAction("get", "delete", "create", "get", "create").
		withExpectedManifestCondition(expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionTrue}, expectedCondition{string(workapiv1.ManifestApplied), metav1.ConditionFalse}).
		withExpectedWorkCondition(expectedCondition{string(workapiv1.WorkApplied), metav1.ConditionFalse})

	work, workKey := spoketesting.NewManifestWork(0, tc.workManifest...)
	work.Finalizers = []string{controllers.ManifestWorkFinalizer}
	controller := newController(work, spoketesting.NewFakeRestMapper()).withKubeObject(tc.spokeObject...).withUnstructuredObject()

	// Add a reactor on fake client to throw error when creating secret on namespace ns2
	controller.kubeClient.PrependReactor("create", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		fmt.Printf("the action get into %v\n", action)
		if action.GetVerb() != "create" {
			return false, nil, nil
		}

		createAction := action.(clienttesting.CreateActionImpl)
		createObject := createAction.Object.(*corev1.Secret)
		if createObject.Namespace == "ns1" {
			return false, createObject, nil
		}

		return true, &corev1.Secret{}, fmt.Errorf("Fake error")
	})
	syncContext := spoketesting.NewFakeSyncContext(t, workKey)
	err := controller.controller.sync(nil, syncContext)
	if err == nil {
		t.Errorf("Should return an err")
	}

	tc.validate(t, controller.dynamicClient, controller.workClient, controller.kubeClient)
}

// Test unstructured compare
func TestIsSameUnstructured(t *testing.T) {
	cases := []struct {
		name     string
		obj1     *unstructured.Unstructured
		obj2     *unstructured.Unstructured
		expected bool
	}{
		{
			name:     "different kind",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind2", "ns1", "n1"),
			expected: false,
		},
		{
			name:     "different namespace",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns2", "n1"),
			expected: false,
		},
		{
			name:     "different name",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n2"),
			expected: false,
		},
		{
			name:     "different spec",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}}),
			expected: false,
		},
		{
			name:     "same spec, different status",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status1"}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status2"}),
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := isSameUnstructured(c.obj1, c.obj2)
			if c.expected != actual {
				t.Errorf("expected %t, but %t", c.expected, actual)
			}
		})
	}
}

func TestGenerateUpdateStatusFunc(t *testing.T) {
	transitionTime := metav1.Now()

	cases := []struct {
		name                     string
		startingStatusConditions []workapiv1.StatusCondition
		manifestConditions       []workapiv1.ManifestCondition
		expectedStatusConditions []workapiv1.StatusCondition
	}{
		{
			name:                     "no manifest condition exists",
			manifestConditions:       []workapiv1.ManifestCondition{},
			expectedStatusConditions: []workapiv1.StatusCondition{},
		},
		{
			name: "all manifests are applied successfully",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", nil),
			},
		},
		{
			name: "one of manifests is not applied",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", nil),
			},
		},
		{
			name: "update existing status condition",
			startingStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", &transitionTime),
			},
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", &transitionTime),
			},
		},
		{
			name: "override existing status conditions",
			startingStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionTrue), "AppliedManifestWorkComplete", "Apply manifest work complete", nil),
			},
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition(string(workapiv1.ManifestApplied), string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expectedStatusConditions: []workapiv1.StatusCondition{
				newCondition(string(workapiv1.WorkApplied), string(metav1.ConditionFalse), "AppliedManifestWorkFailed", "Failed to apply manifest work", nil),
			},
		},
	}

	controller := &ManifestWorkController{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			updateStatusFunc := controller.generateUpdateStatusFunc(workapiv1.ManifestResourceStatus{Manifests: c.manifestConditions})
			manifestWorkStatus := &workapiv1.ManifestWorkStatus{
				Conditions: c.startingStatusConditions,
			}
			err := updateStatusFunc(manifestWorkStatus)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			for i, expect := range c.expectedStatusConditions {
				actual := manifestWorkStatus.Conditions[i]
				if expect.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}

				if !equality.Semantic.DeepEqual(actual, expect) {
					t.Errorf(diff.ObjectDiff(actual, expect))
				}
			}
		})
	}
}

func TestAllInCondition(t *testing.T) {
	cases := []struct {
		name               string
		conditionType      string
		manifestConditions []workapiv1.ManifestCondition
		expected           []bool
	}{
		{
			name:          "condition does not exist",
			conditionType: "one",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{false, false},
		},
		{
			name:          "all manifests are in the condition",
			conditionType: "one",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{true, true},
		},
		{
			name:          "one of manifests is not in the condition",
			conditionType: "two",
			manifestConditions: []workapiv1.ManifestCondition{
				newManifestCondition(0, "resource0", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(1, "resource1", newCondition("one", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(2, "resource0", newCondition("two", string(metav1.ConditionTrue), "my-reason", "my-message", nil)),
				newManifestCondition(3, "resource1", newCondition("two", string(metav1.ConditionFalse), "my-reason", "my-message", nil)),
			},
			expected: []bool{false, true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inCondition, exists := allInCondition(c.conditionType, c.manifestConditions)
			if c.expected[0] != inCondition {
				t.Errorf("expected %t, but %t", c.expected[0], inCondition)
			}

			if c.expected[1] != exists {
				t.Errorf("expected %t, but %t", c.expected[1], exists)
			}
		})
	}
}

func TestBuildManifestResourceMeta(t *testing.T) {
	var secret *corev1.Secret
	var u *unstructured.Unstructured

	cases := []struct {
		name       string
		object     runtime.Object
		restMapper *resource.Mapper
		expected   workapiv1.ManifestResourceMeta
	}{
		{
			name:     "build meta for non-unstructured object",
			object:   spoketesting.NewSecret("test", "ns1", "value2"),
			expected: workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Secret", Namespace: "ns1", Name: "test"},
		},
		{
			name:       "build meta for non-unstructured object with rest mapper",
			object:     spoketesting.NewSecret("test", "ns1", "value2"),
			restMapper: spoketesting.NewFakeRestMapper(),
			expected:   workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Secret", Resource: "secrets", Namespace: "ns1", Name: "test"},
		},
		{
			name:     "build meta for non-unstructured nil",
			object:   secret,
			expected: workapiv1.ManifestResourceMeta{},
		},
		{
			name:     "build meta for unstructured object",
			object:   spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			expected: workapiv1.ManifestResourceMeta{Version: "v1", Kind: "Kind1", Namespace: "ns1", Name: "n1"},
		},
		{
			name:       "build meta for unstructured object with rest mapper",
			object:     spoketesting.NewUnstructured("v1", "NewObject", "ns1", "n1"),
			restMapper: spoketesting.NewFakeRestMapper(),
			expected:   workapiv1.ManifestResourceMeta{Version: "v1", Kind: "NewObject", Resource: "newobjects", Namespace: "ns1", Name: "n1"},
		},
		{
			name:     "build meta for unstructured nil",
			object:   u,
			expected: workapiv1.ManifestResourceMeta{},
		},
		{
			name:     "build meta with nil",
			object:   nil,
			expected: workapiv1.ManifestResourceMeta{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := buildManifestResourceMeta(0, c.object, c.restMapper)
			if err != nil {
				t.Errorf("Should be success with no err: %v", err)
			}

			actual.Ordinal = c.expected.Ordinal
			if !equality.Semantic.DeepEqual(actual, c.expected) {
				t.Errorf(diff.ObjectDiff(actual, c.expected))
			}
		})
	}
}
