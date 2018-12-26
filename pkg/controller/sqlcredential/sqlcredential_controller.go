package sqlcredential

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	sqlv1alpha1 "github.com/coldog/sql-credential-operator/pkg/apis/sql/v1alpha1"
	db "github.com/coldog/sql-credential-operator/pkg/db"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	log   = logf.Log.WithName("controller_sqlcredential")
	newDB = db.New
)

// Add creates a new SQLCredential Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileSQLCredential{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("sqlcredential-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource SQLCredential
	err = c.Watch(&source.Kind{Type: &sqlv1alpha1.SQLCredential{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner SQLCredential
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &sqlv1alpha1.SQLCredential{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileSQLCredential{}

// ReconcileSQLCredential reconciles a SQLCredential object
type ReconcileSQLCredential struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a SQLCredential object and makes changes based on the state read
// and what is in the SQLCredential.Spec
//
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSQLCredential) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling SQLCredential")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Fetch the SQLCredential instance.
	instance := &sqlv1alpha1.SQLCredential{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Spec.Revision <= 0 { // Mega validation check, this would be weird.
		return reconcile.Result{}, fmt.Errorf("Invalid spec revision: %d", instance.Spec.Revision)
	}

	reqLogger = log.WithValues(
		"SQLCredential.Host", instance.Spec.Host,
		"SQLCredential.Driver", instance.Spec.Driver,
		"SQLCredential.Revision", instance.Spec.Revision,
		"SQLCredential.Database", instance.Spec.Database,
		"SQLCredential.User", instance.Spec.User,
		"SQLCredential.Role", instance.Spec.Role,
	)

	// TODO(user): Set values on the reqLogger.

	revision := strconv.FormatInt(instance.Spec.Revision, 10)

	masterURL, err := r.getSecretURL(ctx, instance)
	if err != nil {
		// Error reading master secret - requeue the request.
		reqLogger.Error(err, "Failed to read the master secret")
		return reconcile.Result{}, err
	}

	password, err := genPassword(16) // 32 byte hex string.
	if err != nil {
		// Error reading from crypto/rand - requeue the request.
		reqLogger.Error(err, "Failed to read from crypto/rand")
		return reconcile.Result{}, err
	}

	// Re-assing the password at this stage in case the secret was already created.
	reqLogger.Info("Creating Secret resource")
	password, err = r.createSecret(ctx, reqLogger, instance, password)
	if err != nil {
		// Error writing secret - requeue the request.
		reqLogger.Error(err, "Failed to create secret")
		return reconcile.Result{}, err
	}

	user := db.User{
		Name:     instance.Name + "_" + revision,
		Password: password,
		Role:     instance.Spec.Role,
	}
	db, err := newDB(instance.Spec.Driver, masterURL)
	if err != nil {
		// Error connecting to DB - requeue the request.
		reqLogger.Error(err, "Failed to connect to DB")
		return reconcile.Result{}, err
	}
	defer db.Close()

	reqLogger.Info("Creating User in DB")
	err = db.CreateUser(ctx, user)
	if err != nil {
		// Error writing db - requeue the request.
		reqLogger.Error(err, "Failed to write to the db")
		return reconcile.Result{}, err
	}

	// Garbage collection process will read through all previous revisions and check to
	// find out if deletion is possible.
	reqLogger.Info("Beginning GC process")
	for i := int64(1); i < instance.Spec.Revision; i++ {
		gcRevision := strconv.FormatInt(i, 10)
		gcUser := instance.Spec.User + "_" + gcRevision

		reqLogger = reqLogger.WithValues("GCRevision", gcRevision, "GCUser", gcUser)

		ok, err := db.IsActive(ctx, gcUser)
		if err != nil {
			reqLogger.Error(err, "Failed to check if user active")
			return reconcile.Result{}, err
		}
		if ok { // User exists, reqeueue and try again in a bit.
			reqLogger.Info("User is active, cannot perform GC")
			return reconcile.Result{Requeue: true}, nil
		}

		// Delete the secret for the gcRevision.
		reqLogger.Info("Removing Secret")
		err = r.deleteSecret(ctx, reqLogger, instance, gcRevision)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Remove the user from the DB.
		reqLogger.Info("Removing user")
		err = db.RemoveUser(ctx, gcUser)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileSQLCredential) getSecretURL(ctx context.Context, instance *sqlv1alpha1.SQLCredential) (string, error) {
	found := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{Name: instance.Spec.MasterSecret, Namespace: instance.Namespace}, found)
	if err != nil {
		return "", err
	}
	return b64Decode(found.Data["url"])
}

func (r *ReconcileSQLCredential) createSecret(ctx context.Context, reqLogger logr.Logger, instance *sqlv1alpha1.SQLCredential, password string) (string, error) {
	// Define a new Secret object
	secret := newSecret(instance, password)

	// Set SQLCredential instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, secret, r.scheme); err != nil {
		return "", err
	}

	// Check if this Secret already exists
	found := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Secret", "Secret.Namespace", secret.Namespace, "Secret.Name", secret.Name)
		err = r.client.Create(ctx, secret)
		if err != nil {
			return "", err
		}

		// Secret created successfully - don't requeue
		return "", nil
	} else if err != nil {
		return "", err
	}

	passwordBytes, err := b64Decode(found.Data["password"])
	if err != nil {
		return "", err
	}

	// Secret already exists - don't requeue
	reqLogger.Info("Skip reconcile: Secret already exists", "Secret.Namespace", found.Namespace, "Secret.Name", found.Name)
	return string(passwordBytes), nil
}

func (r *ReconcileSQLCredential) deleteSecret(ctx context.Context, reqLogger logr.Logger, instance *sqlv1alpha1.SQLCredential, revision string) error {
	return nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newSecret(cr *sqlv1alpha1.SQLCredential, password string) *corev1.Secret {
	revision := strconv.FormatInt(cr.Spec.Revision, 10)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-" + revision,
			Namespace: cr.Namespace,
		},
		Data: map[string][]byte{
			"host":     b64(cr.Spec.Host),
			"revision": b64(revision),
			"user":     b64(cr.Spec.User),
			"role":     b64(cr.Spec.Role),
			"password": b64(password),
			"url":      b64(cr.Spec.Driver + "://" + cr.Spec.User + ":" + password + "@" + cr.Spec.Host + "/" + cr.Spec.Database),
		},
	}
}

func b64(in string) []byte {
	return []byte(base64.StdEncoding.EncodeToString([]byte(in)))
}

func b64Decode(in []byte) (string, error) {
	out, err := base64.StdEncoding.DecodeString(string(in))
	return string(out), err
}

func genPassword(s int) (string, error) {
	b, err := genBytes(s)
	return hex.EncodeToString(b), err
}

func genBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}
