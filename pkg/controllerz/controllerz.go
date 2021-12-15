package controllerz

// type KubernetesQueueKey interface {
// 	Namespace() string
// 	Name() string
// }

// var keyFunc cache.KeyFunc = defaultKeyFunc
// var keyFuncSet = make(chan struct{})

// func Key(obj interface{}) (string, error) {
// 	return keyFunc(obj)
// }

// func defaultKeyFunc(obj interface{}) (string, error) {
// 	acc, err := meta.Accessor(obj)
// 	if err != nil {
// 		return "", err
// 	}
// 	return acc.GetNamespace() + "/" + acc.GetName(), nil
// }

// func DecodeKey(key string) KubernetesQueueKey {
// 	return decodeKeyFunc(key)
// }

// type DecodeKeyFunc func(key string) KubernetesQueueKey

// var decodeKeyFunc DecodeKeyFunc = defaultDecodeKeyFunc

// func defaultDecodeKeyFunc(key string) KubernetesQueueKey {
// 	parts := strings.Split(key, "/")
// 	return &kubernetesQueueKey{
// 		namespace: parts[0],
// 		name:      parts[1],
// 	}
// }

// type kubernetesQueueKey struct {
// 	namespace, name string
// }

// func (k *kubernetesQueueKey) Namespace() string {
// 	return k.namespace
// }

// func (k *kubernetesQueueKey) Name() string {
// 	return k.name
// }

// func KeyFunc() cache.KeyFunc {
// 	return keyFunc
// }

// func SetKeyFunc(f cache.KeyFunc) {
// 	keyFunc = f
// 	close(keyFuncSet)
// }

// type AppendFunc func(interface{})

// var LH *ListerHelper

// func Setup(
// 	keyFunc cache.KeyFunc,
// 	dkf DecodeKeyFunc,
// 	listAllIndex string,
// 	listAllIndexValueFunc func(ctx context.Context) string,
// 	listByNamespaceIndex string,
// 	namespaceKeyFunc func(ctx context.Context, ns string) string,
// ) {
// 	SetKeyFunc(keyFunc)
// 	decodeKeyFunc = dkf

// 	LH = NewListerHelper(
// 		listAllIndex,
// 		listAllIndexValueFunc,
// 		listByNamespaceIndex,
// 		namespaceKeyFunc,
// 	)
// }

// type ListerHelper struct {
// 	listAllIndex          string
// 	listAllIndexValueFunc func(ctx context.Context) string
// 	listByNamespaceIndex  string
// 	namespaceKeyFunc      func(ctx context.Context, ns string) string
// }

// func NewListerHelper(
// 	listAllIndex string,
// 	listAllIndexValueFunc func(ctx context.Context) string,
// 	listByNamespaceIndex string,
// 	namespaceKeyFunc func(ctx context.Context, ns string) string,
// ) *ListerHelper {
// 	return &ListerHelper{
// 		listAllIndex:          listAllIndex,
// 		listAllIndexValueFunc: listAllIndexValueFunc,
// 		listByNamespaceIndex:  listByNamespaceIndex,
// 		namespaceKeyFunc:      namespaceKeyFunc,
// 	}
// }

// func (lh *ListerHelper) ListAll(ctx context.Context, indexer cache.Indexer, selector labels.Selector, appendFn AppendFunc) error {
// 	selectAll := selector.Empty()

// 	var (
// 		items []interface{}
// 		err   error
// 	)

// 	if lh.listAllIndex != "" && lh.listAllIndexValueFunc != nil {
// 		items, err = indexer.ByIndex(lh.listAllIndex, lh.listAllIndexValueFunc(ctx))
// 		if err != nil {
// 			return err
// 		}
// 	} else {
// 		items = indexer.List()
// 	}

// 	for _, m := range items {
// 		if selectAll {
// 			// Avoid computing labels of the objects to speed up common flows
// 			// of listing all objects.
// 			appendFn(m)
// 			continue
// 		}
// 		metadata, err := meta.Accessor(m)
// 		if err != nil {
// 			return err
// 		}
// 		if selector.Matches(labels.Set(metadata.GetLabels())) {
// 			appendFn(m)
// 		}
// 	}
// 	return nil
// }

// func (lh *ListerHelper) ListAllByNamespace(ctx context.Context, indexer cache.Indexer, namespace string, selector labels.Selector, appendFn AppendFunc) error {
// 	if namespace == metav1.NamespaceAll {
// 		return lh.ListAll(ctx, indexer, selector, appendFn)
// 	}

// 	selectAll := selector.Empty()

// 	var (
// 		items []interface{}
// 		err   error
// 	)

// 	nsIndex := lh.listByNamespaceIndex
// 	if nsIndex == "" {
// 		nsIndex = cache.NamespaceIndex
// 	}

// 	ns := namespace
// 	if lh.namespaceKeyFunc != nil {
// 		ns = lh.namespaceKeyFunc(ctx, namespace)
// 	}

// 	items, err = indexer.ByIndex(nsIndex, ns)
// 	if err != nil {
// 		return err
// 	}

// 	for _, m := range items {
// 		if selectAll {
// 			// Avoid computing labels of the objects to speed up common flows
// 			// of listing all objects.
// 			appendFn(m)
// 			continue
// 		}
// 		metadata, err := meta.Accessor(m)
// 		if err != nil {
// 			return err
// 		}
// 		if selector.Matches(labels.Set(metadata.GetLabels())) {
// 			appendFn(m)
// 		}
// 	}

// 	return nil
// }

// type syncContextKey int

// const queueKey syncContextKey = iota

// type NewSyncContextFunc func(ctx context.Context, key KubernetesQueueKey) context.Context

// var newSyncContextFunc NewSyncContextFunc = defaultNewSyncContextFunc

// func defaultNewSyncContextFunc(ctx context.Context, key KubernetesQueueKey) context.Context {
// 	return ctx
// }

// func NewSyncContext(ctx context.Context, key KubernetesQueueKey) context.Context {
// 	return newSyncContextFunc(ctx, key)
// }

// func SetNewSyncContextFunc(f NewSyncContextFunc) {
// 	newSyncContextFunc = f
// }

// ////// KCP specific

// type contextKey int

// const clusterKey contextKey = iota

// func ContextWithCluster(ctx context.Context, cluster string) context.Context {
// 	return context.WithValue(ctx, clusterKey, cluster)
// }

// func ClusterFromContext(ctx context.Context) string {
// 	v := ctx.Value(clusterKey)
// 	if v == nil {
// 		return ""
// 	}
// 	return v.(string)
// }

// func KCPNewSyncContext(ctx context.Context, key KubernetesQueueKey) context.Context {
// 	if kcpKey, ok := key.(KCPQueueKeyI); ok {
// 		return ContextWithCluster(ctx, kcpKey.ClusterName())
// 	}
// 	return ctx
// }

// type KCPQueueKeyI interface {
// 	KubernetesQueueKey
// 	ClusterName() string
// }

type KCPQueueKey struct {
	clusterName, namespace, name string
}

func NewKCPQueueKey(clusterName, namespace, name string) *KCPQueueKey {
	return &KCPQueueKey{
		clusterName: clusterName,
		namespace:   namespace,
		name:        name,
	}
}

func (k *KCPQueueKey) ClusterName() string {
	return k.clusterName
}

func (k *KCPQueueKey) Namespace() string {
	return k.namespace
}

func (k *KCPQueueKey) Name() string {
	return k.name
}
