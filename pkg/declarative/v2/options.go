package v2

import (
	"context"
	"os"
	"time"

	"github.com/kyma-project/module-manager/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	FinalizerDefault          = "declarative.kyma-project.io/finalizer"
	FieldOwnerDefault         = "declarative.kyma-project.io/applier"
	EventRecorderDefault      = "declarative.kyma-project.io/events"
	DefaultSkipReconcileLabel = "declarative.kyma-project.io/skip-reconciliation"
)

func DefaultOptions() *Options {
	return (&Options{}).Apply(
		WithDeleteCRDs(false),
		WithNamespace(metav1.NamespaceDefault, false),
		WithFinalizer(FinalizerDefault),
		WithFieldOwner(FieldOwnerDefault),
		WithPostRenderTransform(
			managedByDeclarativeV2,
			kymaComponentTransform,
			disclaimerTransform,
		),
		WithPermanentConsistencyCheck(false),
		WithSingletonClientCache(NewMemorySingletonClientCache()),
		WithManifestCache(os.TempDir()),
		WithSkipReconcileOn(SkipReconcileOnDefaultLabelPresentAndTrue),
	)
}

type Options struct {
	record.EventRecorder
	Config *rest.Config
	client.Client
	TargetClient ClientFn

	SpecResolver
	ClientCache
	ManifestCache
	CustomReadyCheck ReadyCheck

	Namespace       string
	CreateNamespace bool

	Finalizer string

	ServerSideApply bool
	FieldOwner      client.FieldOwner

	PostRenderTransforms []ObjectTransform

	PostRuns   []PostRun
	PreDeletes []PreDelete

	DeletePrerequisites bool

	ShouldSkip SkipReconcile

	CtrlOnSuccess ctrl.Result
}

type Option interface {
	Apply(options *Options)
}

func (o *Options) Apply(options ...Option) *Options {
	for i := range options {
		options[i].Apply(o)
	}
	return o
}

type WithNamespaceOption struct {
	name            string
	createIfMissing bool
}

func WithNamespace(name string, createIfMissing bool) WithNamespaceOption {
	return WithNamespaceOption{
		name:            name,
		createIfMissing: createIfMissing,
	}
}

func (o WithNamespaceOption) Apply(options *Options) {
	options.Namespace = o.name
	options.CreateNamespace = o.createIfMissing
}

type WithFieldOwner client.FieldOwner

func (o WithFieldOwner) Apply(options *Options) {
	options.FieldOwner = client.FieldOwner(o)
}

type WithFinalizer string

func (o WithFinalizer) Apply(options *Options) {
	options.Finalizer = string(o)
}

type WithManagerOption struct {
	manager.Manager
}

func WithManager(mgr manager.Manager) WithManagerOption {
	return WithManagerOption{Manager: mgr}
}

func (o WithManagerOption) Apply(options *Options) {
	options.EventRecorder = o.GetEventRecorderFor(EventRecorderDefault)
	options.Config = o.GetConfig()
	options.Client = o.GetClient()
}

type WithCustomResourceLabels labels.Set

func (o WithCustomResourceLabels) Apply(options *Options) {
	labelTransform := func(ctx context.Context, object Object, resources []*unstructured.Unstructured) error {
		for _, targetResource := range resources {
			lbls := targetResource.GetLabels()
			if lbls == nil {
				lbls = labels.Set{}
			}
			for s := range o {
				lbls[s] = o[s]
			}
			targetResource.SetLabels(lbls)
		}
		return nil
	}
	options.PostRenderTransforms = append(options.PostRenderTransforms, labelTransform)
}

func WithSpecResolver(resolver SpecResolver) SpecResolverOption {
	return SpecResolverOption{resolver}
}

type SpecResolverOption struct {
	SpecResolver
}

func (o SpecResolverOption) Apply(options *Options) {
	options.SpecResolver = o
}

type ObjectTransform = func(context.Context, Object, []*unstructured.Unstructured) error

func WithPostRenderTransform(transforms ...ObjectTransform) PostRenderTransformOption {
	return PostRenderTransformOption{transforms}
}

type PostRenderTransformOption struct {
	ObjectTransforms []ObjectTransform
}

func (o PostRenderTransformOption) Apply(options *Options) {
	options.PostRenderTransforms = append(options.PostRenderTransforms, o.ObjectTransforms...)
}

// Hook defines a Hook into the declarative reconciliation
// skr is the runtime cluster
// kcp is the control-plane cluster
// obj is guaranteed to be the reconciled object and also to always preside in kcp.
type Hook func(ctx context.Context, skr Client, kcp client.Client, obj Object) error

// WARNING: DO NOT USE THESE HOOKS IF YOU DO NOT KNOW THE RECONCILIATION LIFECYCLE OF THE DECLARATIVE API.
// IT CAN BREAK YOUR RECONCILIATION AND IF YOU ADJUST THE OBJECT, ALSO LEAD TO
// INVALID STATES.
type (
	// PostRun is executed after every successful render+reconciliation of the manifest.
	PostRun Hook
	// PreDelete is executed before any deletion of resources calculated from the status.
	PreDelete Hook
)

// WithPostRun applies PostRun.
type WithPostRun []PostRun

func (o WithPostRun) Apply(options *Options) {
	options.PostRuns = append(options.PostRuns, o...)
}

// WithPreDelete applies PreDelete.
type WithPreDelete []PreDelete

func (o WithPreDelete) Apply(options *Options) {
	options.PreDeletes = append(options.PreDeletes, o...)
}

type WithPeriodicConsistencyCheck time.Duration

func (o WithPeriodicConsistencyCheck) Apply(options *Options) {
	options.CtrlOnSuccess.RequeueAfter = time.Duration(o)
}

type WithPermanentConsistencyCheck bool

func (o WithPermanentConsistencyCheck) Apply(options *Options) {
	if o {
		options.CtrlOnSuccess = ctrl.Result{Requeue: true}
	} else {
		options.CtrlOnSuccess = ctrl.Result{}
	}
}

type WithSingletonClientCacheOption struct {
	ClientCache
}

func WithSingletonClientCache(cache ClientCache) WithSingletonClientCacheOption {
	return WithSingletonClientCacheOption{ClientCache: cache}
}

func (o WithSingletonClientCacheOption) Apply(options *Options) {
	options.ClientCache = o
}

type WithDeleteCRDs bool

func (o WithDeleteCRDs) Apply(options *Options) {
	options.DeletePrerequisites = bool(o)
}

type ManifestCache string

const NoManifestCache ManifestCache = "no-cache"

type WithManifestCache ManifestCache

func (o WithManifestCache) Apply(options *Options) {
	options.ManifestCache = ManifestCache(o)
}

type WithCustomReadyCheckOption struct {
	ReadyCheck
}

func WithCustomReadyCheck(check ReadyCheck) WithCustomReadyCheckOption {
	return WithCustomReadyCheckOption{ReadyCheck: check}
}

func (o WithCustomReadyCheckOption) Apply(options *Options) {
	options.CustomReadyCheck = o
}

type ClientFn func(context.Context, Object) (client.Client, error)

func WithRemoteTargetCluster(clientFn ClientFn) WithRemoteTargetClusterOption {
	return WithRemoteTargetClusterOption{ClientFn: clientFn}
}

type WithRemoteTargetClusterOption struct {
	ClientFn func(context.Context, Object) (client.Client, error)
}

func (o WithRemoteTargetClusterOption) Apply(options *Options) {
	options.TargetClient = o.ClientFn
}

func WithSkipReconcileOn(skipReconcile SkipReconcile) WithSkipReconcileOnOption {
	return WithSkipReconcileOnOption{skipReconcile: skipReconcile}
}

type SkipReconcile func(context.Context, Object) (skip bool)

// SkipReconcileOnDefaultLabelPresentAndTrue determines SkipReconcile by checking if DefaultSkipReconcileLabel is true.
func SkipReconcileOnDefaultLabelPresentAndTrue(ctx context.Context, object Object) bool {
	log.FromContext(ctx, "skip-label", DefaultSkipReconcileLabel).
		V(util.DebugLogLevel).Info("resource gets skipped because of label")
	return object.GetLabels() != nil && object.GetLabels()[DefaultSkipReconcileLabel] == "true"
}

type WithSkipReconcileOnOption struct {
	skipReconcile SkipReconcile
}

func (o WithSkipReconcileOnOption) Apply(options *Options) {
	options.ShouldSkip = o.skipReconcile
}
