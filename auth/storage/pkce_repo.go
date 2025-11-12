package storage

import (
	"context"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils/storageutils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var pkceRepo *PKCERepo

// PKCERepo manages PKCEState data in MongoDB
type PKCERepo struct {
	Repo storageutils.Repoable
}

// GetPKCERepo returns a singleton instance of PKCERepo
func GetPKCERepo() (*PKCERepo, error) {
	if pkceRepo != nil {
		return pkceRepo, nil
	}

	st, err := storageutils.New("pantahub_")
	if err != nil {
		return nil, err
	}

	collection := st.GetCollection(PKCEServicePrn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	f := false
	t := true

	_, err = collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: map[string]int{
				"auth_code": 1,
			},
			Options: &options.IndexOptions{
				Unique:     &t,
				Background: &f,
				Sparse:     &f,
			},
		},
		{
			Keys: map[string]int{
				"expires_at": 1,
			},
			Options: &options.IndexOptions{
				Unique:     &f,
				Background: &f,
				Sparse:     &f,
			},
		},
		{
			Keys: map[string]int{
				"is_used": 1,
			},
			Options: &options.IndexOptions{
				Unique:     &f,
				Background: &f,
				Sparse:     &f,
			},
		},
	})

	if err != nil {
		return nil, err
	}

	pkceRepo = &PKCERepo{
		Repo: &storageutils.Repo{
			Collection: collection,
			Storage:    st,
		},
	}

	return pkceRepo, nil
}

// Create inserts a new PKCEState into the database
func (db *PKCERepo) Create(ctx context.Context, pks *PKCEState) error {
	return db.Repo.Insert(ctx, pks)
}

// FindByAuthCode retrieves a PKCEState by its auth_code
func (db *PKCERepo) FindByAuthCode(ctx context.Context, authCode string) (*PKCEState, error) {
	pks := &PKCEState{}
	err := db.Repo.FindBy(ctx, "auth_code", authCode, pks)
	return pks, err
}

// Update updates an existing PKCEState in the database
func (db *PKCERepo) Update(ctx context.Context, pks *PKCEState) error {
	return db.Repo.UpdateOne(ctx, pks, false)
}

// Delete deletes a PKCEState from the database by its auth_code
func (db *PKCERepo) Delete(ctx context.Context, authCode string) error {
	query := bson.M{"auth_code": authCode}
	return db.Repo.DeleteMany(ctx, query)
}

func (db *PKCERepo) DeleteExpired(ctx context.Context) error {
	now := time.Now()
	query := bson.M{"expires_at": bson.M{"$lt": now}}
	return db.Repo.DeleteMany(ctx, query)
}
