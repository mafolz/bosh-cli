package stemcell_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	fakesys "github.com/cloudfoundry/bosh-agent/system/fakes"

	bmeventlog "github.com/cloudfoundry/bosh-micro-cli/eventlogger"

	fakebmcloud "github.com/cloudfoundry/bosh-micro-cli/cloud/fakes"
	fakebmstemcell "github.com/cloudfoundry/bosh-micro-cli/deployer/stemcell/fakes"
	fakebmlog "github.com/cloudfoundry/bosh-micro-cli/eventlogger/fakes"

	. "github.com/cloudfoundry/bosh-micro-cli/deployer/stemcell"
)

var _ = Describe("Manager", func() {
	var (
		repo                *fakebmstemcell.FakeRepo
		manager             Manager
		fs                  *fakesys.FakeFileSystem
		reader              *fakebmstemcell.FakeStemcellReader
		fakeCloud           *fakebmcloud.FakeCloud
		fakeStage           *fakebmlog.FakeStage
		eventLogger         *fakebmlog.FakeEventLogger
		stemcellTarballPath string
		tempExtractionDir   string

		expectedExtractedStemcell ExtractedStemcell
	)

	BeforeEach(func() {
		fs = fakesys.NewFakeFileSystem()
		reader = fakebmstemcell.NewFakeReader()
		repo = fakebmstemcell.NewFakeRepo()
		eventLogger = fakebmlog.NewFakeEventLogger()
		fakeStage = fakebmlog.NewFakeStage()
		eventLogger.SetNewStageBehavior(fakeStage)
		fakeCloud = fakebmcloud.NewFakeCloud()
		manager = NewManager(repo, fakeCloud, eventLogger)
		stemcellTarballPath = "/stemcell/tarball/path"
		tempExtractionDir = "/path/to/dest"
		fs.TempDirDir = tempExtractionDir

		expectedExtractedStemcell = NewExtractedStemcell(
			Manifest{
				Name:      "fake-stemcell-name",
				ImagePath: "fake-image-path",
				RawCloudProperties: map[interface{}]interface{}{
					"fake-prop-key": "fake-prop-value",
				},
			},
			ApplySpec{},
			tempExtractionDir,
			fs,
		)
		reader.SetReadBehavior(stemcellTarballPath, tempExtractionDir, expectedExtractedStemcell, nil)
	})

	Describe("Upload", func() {
		var (
			foundCloudStemcell    CloudStemcell
			expectedCloudStemcell CloudStemcell
		)
		BeforeEach(func() {
			foundCloudStemcell = CloudStemcell{}

			// no existing stemcell found
			repo.SetFindBehavior(expectedExtractedStemcell.Manifest(), foundCloudStemcell, false, nil)

			fakeCloud.CreateStemcellCID = "fake-stemcell-cid"

			expectedCloudStemcell = CloudStemcell{CID: "fake-stemcell-cid"}

			repo.SetSaveBehavior(expectedExtractedStemcell.Manifest(), expectedCloudStemcell, nil)
		})

		It("starts a new event logger stage", func() {
			_, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).ToNot(HaveOccurred())

			Expect(eventLogger.NewStageInputs).To(Equal([]fakebmlog.NewStageInput{
				{
					Name: "uploading stemcell",
				},
			}))

			Expect(fakeStage.Started).To(BeTrue())
			Expect(fakeStage.Finished).To(BeTrue())
		})

		It("checks that the stemcell has not already been uploaded", func() {
			_, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).ToNot(HaveOccurred())

			Expect(repo.FindInputs).To(Equal([]fakebmstemcell.FindInput{
				{
					StemcellManifest: expectedExtractedStemcell.Manifest(),
				},
			}))
		})

		It("uploads the stemcell to the infrastructure and returns the cid", func() {
			cloudStemcell, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).ToNot(HaveOccurred())
			Expect(cloudStemcell).To(Equal(expectedCloudStemcell))

			Expect(fakeCloud.CreateStemcellInputs).To(Equal([]fakebmcloud.CreateStemcellInput{
				{
					CloudProperties: map[string]interface{}{
						"fake-prop-key": "fake-prop-value",
					},
					ImagePath: "fake-image-path",
				},
			}))
		})

		It("saves the stemcell record in the repo", func() {
			cloudStemcell, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).ToNot(HaveOccurred())
			Expect(cloudStemcell).To(Equal(expectedCloudStemcell))

			Expect(repo.SaveInputs).To(Equal([]fakebmstemcell.SaveInput{
				{
					StemcellManifest: expectedExtractedStemcell.Manifest(),
					Stemcell:         cloudStemcell,
				},
			}))
		})

		It("logs uploading start and stop events to the eventLogger", func() {
			_, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).ToNot(HaveOccurred())

			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Uploading",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Finished,
				},
			}))
		})

		It("when the upload fails, logs uploading start and failure events to the eventLogger", func() {
			fakeCloud.CreateStemcellErr = errors.New("fake-create-error")
			_, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-create-error"))

			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Uploading",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Failed,
				},
				FailMessage: "fake-create-error",
			}))
		})

		It("when the repo save fails, logs uploading start and failure events to the eventLogger", func() {
			repo.SetSaveBehavior(
				expectedExtractedStemcell.Manifest(),
				expectedCloudStemcell,
				errors.New("fake-save-error"),
			)

			_, err := manager.Upload(expectedExtractedStemcell)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-save-error"))

			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Uploading",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Failed,
				},
				FailMessage: "fake-save-error",
			}))
		})

		Context("when the stemcell record exists in the repo (having been previously uploaded)", func() {
			var (
				existingCloudStemcell CloudStemcell
			)

			BeforeEach(func() {
				existingCloudStemcell = CloudStemcell{CID: "fake-existing-cid"}
				repo.SetFindBehavior(expectedExtractedStemcell.Manifest(), existingCloudStemcell, true, nil)
			})

			It("returns the existing cloud stemcell", func() {
				stemcell, err := manager.Upload(expectedExtractedStemcell)
				Expect(err).ToNot(HaveOccurred())
				Expect(stemcell).To(Equal(existingCloudStemcell))
			})

			It("does not re-upload the stemcell to the infrastructure", func() {
				_, err := manager.Upload(expectedExtractedStemcell)
				Expect(err).ToNot(HaveOccurred())
				Expect(fakeCloud.CreateStemcellInputs).To(HaveLen(0))
			})

			It("logs skipping uploading events to the eventLogger", func() {
				_, err := manager.Upload(expectedExtractedStemcell)
				Expect(err).ToNot(HaveOccurred())

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Uploading",
					States: []bmeventlog.EventState{
						bmeventlog.Skipped,
					},
					SkipMessage: "Stemcell already uploaded",
				}))
			})
		})
	})
})
