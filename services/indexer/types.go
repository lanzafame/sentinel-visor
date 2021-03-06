package indexer

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/api/global"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/sentinel-visor/model/blocks"
)

type ActorTips map[types.TipSetKey][]ActorInfo

type ActorInfo struct {
	Actor           types.Actor
	Address         address.Address
	ParentStateRoot cid.Cid
	TipSet          types.TipSetKey
	ParentTipSet    types.TipSetKey
}

func NewUnindexedBlockData() *UnindexedBlockData {
	return &UnindexedBlockData{
		has: make(map[cid.Cid]struct{}),
	}
}

// TODO put this somewhere else, maybe in the model?
type UnindexedBlockData struct {
	has     map[cid.Cid]struct{}
	highest *types.BlockHeader

	blks              blocks.BlockHeaders
	synced            blocks.BlocksSynced
	parents           blocks.BlockParents
	drandEntries      blocks.DrandEntries
	drandBlockEntries blocks.DrandBlockEntries
}

func (u *UnindexedBlockData) Highest() (cid.Cid, int64) {
	return u.highest.Cid(), int64(u.highest.Height)
}

func (u *UnindexedBlockData) Add(bh *types.BlockHeader) {
	u.has[bh.Cid()] = struct{}{}

	if u.highest == nil {
		u.highest = bh
	} else if u.highest.Height < bh.Height {
		u.highest = bh
	}

	u.blks = append(u.blks, blocks.NewBlockHeader(bh))
	u.synced = append(u.synced, blocks.NewBlockSynced(bh))
	u.parents = append(u.parents, blocks.NewBlockParents(bh)...)
	u.drandEntries = append(u.drandEntries, blocks.NewDrandEnties(bh)...)
	u.drandBlockEntries = append(u.drandBlockEntries, blocks.NewDrandBlockEntries(bh)...)
}

func (u *UnindexedBlockData) Has(bh *types.BlockHeader) bool {
	_, has := u.has[bh.Cid()]
	return has
}

func (u *UnindexedBlockData) Persist(ctx context.Context, db *pg.DB) error {
	ctx, span := global.Tracer("").Start(ctx, "Indexer.PersistBlockData")
	defer span.End()

	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		log.Infow("Persist unindexed block data", "count", u.Size())
		grp, ctx := errgroup.WithContext(ctx)

		grp.Go(func() error {
			if err := u.blks.PersistWithTx(ctx, tx); err != nil {
				return xerrors.Errorf("persist block headers: %w", err)
			}
			return nil
		})

		grp.Go(func() error {
			if err := u.synced.PersistWithTx(ctx, tx); err != nil {
				return xerrors.Errorf("persist blocks synced: %w", err)
			}
			return nil
		})

		grp.Go(func() error {
			if err := u.parents.PersistWithTx(ctx, tx); err != nil {
				return xerrors.Errorf("persist block parents: %w", err)
			}
			return nil
		})

		grp.Go(func() error {
			if err := u.drandEntries.PersistWithTx(ctx, tx); err != nil {
				return xerrors.Errorf("persist drand entries: %w", err)
			}
			return nil
		})

		grp.Go(func() error {
			if err := u.drandBlockEntries.PersistWithTx(ctx, tx); err != nil {
				return xerrors.Errorf("persist drand block entries: %w", err)
			}
			return nil
		})

		if err := grp.Wait(); err != nil {
			log.Info("Rolling back unindexed block data", "error", err)
			return err
		}

		log.Info("Committing unindexed block data")
		return nil
	})
}

func (u *UnindexedBlockData) Size() int {
	return len(u.has)
}
