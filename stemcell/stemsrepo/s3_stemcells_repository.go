package stemsrepo

import (
	"strings"

	bosherr "github.com/cloudfoundry/bosh-agent/errors"
	boshlog "github.com/cloudfoundry/bosh-agent/logger"
	bpindex "github.com/cppforlife/bosh-provisioner/index"

	bhchecks "github.com/cppforlife/bosh-hub/checksumsrepo"
	bhs3 "github.com/cppforlife/bosh-hub/s3"
	bhnotesrepo "github.com/cppforlife/bosh-hub/stemcell/notesrepo"
)

var (
	// s3StemcellsKeyLatest is the single key
	// used to keep S3 bucket results
	s3StemcellsKeyLatest = s3StemcellsKey{"latest"}
)

type S3StemcellsRepository struct {
	index         bpindex.Index
	checksumsRepo bhchecks.ChecksumsRepository
	notesRepo     bhnotesrepo.NotesRepository
	logger        boshlog.Logger
}

type s3StemcellsKey struct {
	Key string
}

type s3StemcellRec struct {
	Key  string
	ETag string
	SHA1 string

	Size         uint64
	LastModified string

	URL string
}

func NewS3StemcellsRepository(
	index bpindex.Index,
	checksumsRepo bhchecks.ChecksumsRepository,
	notesRepo bhnotesrepo.NotesRepository,
	logger boshlog.Logger,
) S3StemcellsRepository {
	return S3StemcellsRepository{
		index:         index,
		checksumsRepo: checksumsRepo,
		notesRepo:     notesRepo,
		logger:        logger,
	}
}

func (r S3StemcellsRepository) FindAll(name string) ([]Stemcell, error) {
	var stems []Stemcell

	var s3StemcellRecs []s3StemcellRec

	err := r.index.Find(s3StemcellsKeyLatest, &s3StemcellRecs)
	if err != nil {
		if err == bpindex.ErrNotFound {
			return stems, nil
		}

		return stems, bosherr.WrapError(err, "Finding all S3 stemcell recs")
	}

	filterByName := len(name) > 0

	for _, rec := range s3StemcellRecs {
		stemcell := NewS3Stemcell(
			rec.Key,
			rec.ETag,
			rec.SHA1,
			rec.Size,
			rec.LastModified,
			rec.URL,
		)

		if stemcell == nil || stemcell.IsDeprecated() {
			continue
		}

		stemcell.notesRepo = r.notesRepo

		if !filterByName || filterByName && stemcell.Name() == name {
			stems = append(stems, stemcell)
		}
	}

	return stems, nil
}

func (r S3StemcellsRepository) SaveAll(s3Files []bhs3.File) error {
	var s3StemcellRecs []s3StemcellRec

	for _, s3File := range s3Files {
		url, err := s3File.URL()
		if err != nil {
			return bosherr.WrapError(err, "Generating S3 stemcell URL")
		}

		rec := s3StemcellRec{
			Key:  s3File.Key(),
			ETag: s3File.ETag(),

			Size:         s3File.Size(),
			LastModified: s3File.LastModified(),

			URL: url,
		}

		stemcell := NewS3Stemcell(rec.Key, "", "", 0, "", "")

		// Skip S3 files that dont look like stemcells
		if stemcell == nil {
			continue
		}

		// Record SHA1 for each found stemcell
		rec.SHA1, err = r.findStemcellSHA1(s3File)

		if err != nil && stemcell.MustHaveSHA1() {
			if stemcell.InfName() == "softlayer" {
				continue
			}

			return err
		}

		s3StemcellRecs = append(s3StemcellRecs, rec)
	}

	err := r.index.Save(s3StemcellsKeyLatest, s3StemcellRecs)
	if err != nil {
		return bosherr.WrapError(err, "Saving all S3 stemcell recs")
	}

	return nil
}

func (r S3StemcellsRepository) findStemcellSHA1(file bhs3.File) (string, error) {
	checksumKeyParts := strings.Split(file.Key(), "/")
	checksumKey := checksumKeyParts[len(checksumKeyParts)-1]

	checksum, findErr := r.checksumsRepo.Find(checksumKey)
	if findErr != nil {
		return "", bosherr.WrapError(findErr, "Fetching SHA1 for stemcell '%s'", checksumKey)
	}

	if len(checksum.SHA1) == 0 {
		return "", bosherr.New("Expected to find non-empty SHA1 for stemcell '%s'", checksumKey)
	}

	return checksum.SHA1, nil
}
