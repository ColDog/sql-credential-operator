package sqlcredential

import (
	"context"
	"database/sql"
	"testing"

	sqlv1alpha1 "github.com/coldog/sql-credential-operator/pkg/apis/sql/v1alpha1"
	"github.com/coldog/sql-credential-operator/pkg/db"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

type mockDB struct {
	db.DB
	mock.Mock
}

func (m *mockDB) Close() {}

func (m *mockDB) CreateUser(ctx context.Context, user db.User) error {
	args := m.Called(user)
	return args.Error(0)
}

func (m *mockDB) RemoveUser(ctx context.Context, name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *mockDB) IsActive(ctx context.Context, name string) (bool, error) {
	args := m.Called(name)
	return args.Bool(0), args.Error(1)
}

func setup(t *testing.T, crd *sqlv1alpha1.SQLCredential) (*mockDB, reconcile.Request, *ReconcileSQLCredential) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(logf.ZapLogger(true))

	mockedDB := &mockDB{}

	newDB = func(driver, url string) (db.DB, error) {
		return mockedDB, nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "master", Namespace: "default"},
		Data: map[string][]byte{
			"url": []byte("dGVzdA=="),
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{crd, secret}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(sqlv1alpha1.SchemeGroupVersion, crd)
	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &ReconcileSQLCredential{client: cl, scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      crd.Name,
			Namespace: crd.Namespace,
		},
	}
	return mockedDB, req, r
}

func TestController_Success(t *testing.T) {
	db, req, r := setup(t, &sqlv1alpha1.SQLCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: sqlv1alpha1.SQLCredentialSpec{
			Revision:     1,
			MasterSecret: "master",

			User:     "testuser",
			Host:     "dbhost",
			Database: "testdb",
			Role:     "testrole",
			Driver:   "mocked",
		},
	})

	db.On("CreateUser", mock.Anything).Return(nil)
	// db.On("RemoveUser", mock.Anything).Return(nil)
	// db.On("IsActive", mock.Anything).Return(false, nil)

	res, err := r.Reconcile(req)

	{
		found := &corev1.Secret{}
		err := r.client.Get(context.Background(), types.NamespacedName{
			Name:      "test-1",
			Namespace: "default",
		}, found)
		require.NoError(t, err)
		require.NotEmpty(t, found.Data["password"])
	}

	require.NoError(t, err)
	require.False(t, res.Requeue)
	db.AssertExpectations(t)
}

func TestController_FailedCreate(t *testing.T) {
	db, req, r := setup(t, &sqlv1alpha1.SQLCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: sqlv1alpha1.SQLCredentialSpec{
			Revision:     1,
			MasterSecret: "master",

			User:     "testuser",
			Host:     "dbhost",
			Database: "testdb",
			Role:     "testrole",
			Driver:   "mocked",
		},
	})

	db.On("CreateUser", mock.Anything).Return(sql.ErrTxDone)
	// db.On("RemoveUser", mock.Anything).Return(nil)
	// db.On("IsActive", mock.Anything).Return(false, nil)

	res, err := r.Reconcile(req)

	{
		found := &corev1.Secret{}
		err := r.client.Get(context.Background(), types.NamespacedName{
			Name:      "test-1",
			Namespace: "default",
		}, found)
		require.NoError(t, err)
		require.NotEmpty(t, found.Data["password"])
	}

	require.Error(t, err)
	require.False(t, res.Requeue)
	db.AssertExpectations(t)
}

func TestController_FailedCreateReExec(t *testing.T) {
	mockedDB, req, r := setup(t, &sqlv1alpha1.SQLCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: sqlv1alpha1.SQLCredentialSpec{
			Revision:     1,
			MasterSecret: "master",

			User:     "testuser",
			Host:     "dbhost",
			Database: "testdb",
			Role:     "testrole",
			Driver:   "mocked",
		},
	})

	mockedDB.On("CreateUser", mock.Anything).Return(sql.ErrTxDone)

	res, err := r.Reconcile(req)
	require.Error(t, err)
	require.False(t, res.Requeue)
	mockedDB.AssertExpectations(t)

	{
		found := &corev1.Secret{}
		err := r.client.Get(context.Background(), types.NamespacedName{
			Name:      "test-1",
			Namespace: "default",
		}, found)
		require.NoError(t, err)
		require.NotEmpty(t, found.Data["password"])
	}

	mockedDB = &mockDB{}
	newDB = func(driver, url string) (db.DB, error) {
		return mockedDB, nil
	}

	mockedDB.On("CreateUser", mock.Anything).Return(nil)

	res, err = r.Reconcile(req)
	require.NoError(t, err)
	require.False(t, res.Requeue)
	mockedDB.AssertExpectations(t)
}

func TestController_GCSuccess(t *testing.T) {
	db, req, r := setup(t, &sqlv1alpha1.SQLCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: sqlv1alpha1.SQLCredentialSpec{
			Revision:     2,
			MasterSecret: "master",

			User:     "testuser",
			Host:     "dbhost",
			Database: "testdb",
			Role:     "testrole",
			Driver:   "mocked",
		},
	})

	db.On("CreateUser", mock.Anything).Return(nil)
	db.On("RemoveUser", "testuser_1").Return(nil)
	db.On("IsActive", "testuser_1").Return(false, nil)

	res, err := r.Reconcile(req)

	{
		found := &corev1.Secret{}
		err := r.client.Get(context.Background(), types.NamespacedName{
			Name:      "test-2",
			Namespace: "default",
		}, found)
		require.NoError(t, err)
		require.NotEmpty(t, found.Data["password"])
	}

	require.NoError(t, err)
	require.False(t, res.Requeue)
	db.AssertExpectations(t)
}

func TestController_GCActive(t *testing.T) {
	db, req, r := setup(t, &sqlv1alpha1.SQLCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: sqlv1alpha1.SQLCredentialSpec{
			Revision:     2,
			MasterSecret: "master",

			User:     "testuser",
			Host:     "dbhost",
			Database: "testdb",
			Role:     "testrole",
			Driver:   "mocked",
		},
	})

	db.On("CreateUser", mock.Anything).Return(nil)
	// db.On("RemoveUser", "testuser_1").Return(nil) <- Not Set!
	db.On("IsActive", "testuser_1").Return(true, nil)

	res, err := r.Reconcile(req)

	{
		found := &corev1.Secret{}
		err := r.client.Get(context.Background(), types.NamespacedName{
			Name:      "test-2",
			Namespace: "default",
		}, found)
		require.NoError(t, err)
		require.NotEmpty(t, found.Data["password"])
	}

	require.NoError(t, err)
	require.True(t, res.Requeue) // <- Will be retried.
	db.AssertExpectations(t)
}
