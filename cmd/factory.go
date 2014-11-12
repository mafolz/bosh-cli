package cmd

import (
	"errors"
	"time"

	boshblob "github.com/cloudfoundry/bosh-agent/blobstore"
	bosherr "github.com/cloudfoundry/bosh-agent/errors"
	boshlog "github.com/cloudfoundry/bosh-agent/logger"
	boshcmd "github.com/cloudfoundry/bosh-agent/platform/commands"
	boshsys "github.com/cloudfoundry/bosh-agent/system"
	boshtime "github.com/cloudfoundry/bosh-agent/time"
	boshuuid "github.com/cloudfoundry/bosh-agent/uuid"

	bmcloud "github.com/cloudfoundry/bosh-micro-cli/cloud"
	bmconfig "github.com/cloudfoundry/bosh-micro-cli/config"
	bmcpi "github.com/cloudfoundry/bosh-micro-cli/cpi"
	bmcomp "github.com/cloudfoundry/bosh-micro-cli/cpi/compile"
	bmcpiinstall "github.com/cloudfoundry/bosh-micro-cli/cpi/install"
	bmpkgs "github.com/cloudfoundry/bosh-micro-cli/cpi/packages"
	bmdeployer "github.com/cloudfoundry/bosh-micro-cli/deployer"
	bmagentclient "github.com/cloudfoundry/bosh-micro-cli/deployer/agentclient"
	bmas "github.com/cloudfoundry/bosh-micro-cli/deployer/applyspec"
	bmblobstore "github.com/cloudfoundry/bosh-micro-cli/deployer/blobstore"
	bmdisk "github.com/cloudfoundry/bosh-micro-cli/deployer/disk"
	bmregistry "github.com/cloudfoundry/bosh-micro-cli/deployer/registry"
	bmsshtunnel "github.com/cloudfoundry/bosh-micro-cli/deployer/sshtunnel"
	bmstemcell "github.com/cloudfoundry/bosh-micro-cli/deployer/stemcell"
	bmvm "github.com/cloudfoundry/bosh-micro-cli/deployer/vm"
	bmdepl "github.com/cloudfoundry/bosh-micro-cli/deployment"
	bmdeplval "github.com/cloudfoundry/bosh-micro-cli/deployment/validator"
	bmeventlog "github.com/cloudfoundry/bosh-micro-cli/eventlogger"
	bmindex "github.com/cloudfoundry/bosh-micro-cli/index"
	bmrelvalidation "github.com/cloudfoundry/bosh-micro-cli/release/validation"
	bmtempcomp "github.com/cloudfoundry/bosh-micro-cli/templatescompiler"
	bmerbrenderer "github.com/cloudfoundry/bosh-micro-cli/templatescompiler/erbrenderer"
	bmui "github.com/cloudfoundry/bosh-micro-cli/ui"
)

type Factory interface {
	CreateCommand(name string) (Cmd, error)
}

type factory struct {
	commands                map[string](func() (Cmd, error))
	userConfig              bmconfig.UserConfig
	userConfigService       bmconfig.UserConfigService
	deploymentConfig        bmconfig.DeploymentConfig
	deploymentConfigService bmconfig.DeploymentConfigService
	fs                      boshsys.FileSystem
	ui                      bmui.UI
	logger                  boshlog.Logger
	uuidGenerator           boshuuid.Generator
	workspace               string
}

func NewFactory(
	userConfig bmconfig.UserConfig,
	userConfigService bmconfig.UserConfigService,
	fs boshsys.FileSystem,
	ui bmui.UI,
	logger boshlog.Logger,
	uuidGenerator boshuuid.Generator,
	workspace string,
) Factory {
	f := &factory{
		userConfig:        userConfig,
		userConfigService: userConfigService,
		fs:                fs,
		ui:                ui,
		logger:            logger,
		uuidGenerator:     uuidGenerator,
		workspace:         workspace,
	}
	f.loadDeploymentConfig()
	f.commands = map[string](func() (Cmd, error)){
		"deployment": f.createDeploymentCmd,
		"deploy":     f.createDeployCmd,
	}
	return f
}

func (f *factory) CreateCommand(name string) (Cmd, error) {
	if f.commands[name] == nil {
		return nil, errors.New("Invalid command name")
	}

	return f.commands[name]()
}

func (f *factory) createDeploymentCmd() (Cmd, error) {
	return NewDeploymentCmd(
		f.ui,
		f.userConfig,
		f.userConfigService,
		f.deploymentConfig,
		f.fs,
		f.uuidGenerator,
		f.logger,
	), nil
}

func (f *factory) createDeployCmd() (Cmd, error) {
	runner := boshsys.NewExecCmdRunner(f.logger)
	extractor := boshcmd.NewTarballCompressor(runner, f.fs)

	boshValidator := bmrelvalidation.NewBoshValidator(f.fs)
	cpiReleaseValidator := bmrelvalidation.NewCpiValidator()
	releaseValidator := bmrelvalidation.NewValidator(
		boshValidator,
		cpiReleaseValidator,
		f.ui,
	)

	compressor := boshcmd.NewTarballCompressor(runner, f.fs)
	indexFilePath := f.deploymentConfig.CompiledPackagedIndexPath()
	compiledPackageIndex := bmindex.NewFileIndex(indexFilePath, f.fs)
	compiledPackageRepo := bmpkgs.NewCompiledPackageRepo(compiledPackageIndex)

	options := map[string]interface{}{"blobstore_path": f.deploymentConfig.BlobstorePath()}
	blobstore := boshblob.NewSHA1VerifiableBlobstore(
		boshblob.NewLocalBlobstore(f.fs, f.uuidGenerator, options),
	)
	blobExtractor := bmcpiinstall.NewBlobExtractor(f.fs, compressor, blobstore, f.logger)
	packageInstaller := bmcpiinstall.NewPackageInstaller(compiledPackageRepo, blobExtractor)
	packageCompiler := bmcomp.NewPackageCompiler(
		runner,
		f.deploymentConfig.PackagesPath(),
		f.fs,
		compressor,
		blobstore,
		compiledPackageRepo,
		packageInstaller,
	)
	timeService := boshtime.NewConcreteService()
	eventFilters := []bmeventlog.EventFilter{
		bmeventlog.NewTimeFilter(timeService),
	}
	eventLogger := bmeventlog.NewEventLoggerWithFilters(f.ui, eventFilters)

	da := bmcomp.NewDependencyAnalysis()
	releasePackagesCompiler := bmcomp.NewReleasePackagesCompiler(
		da,
		packageCompiler,
		eventLogger,
		timeService,
	)

	cpiManifestParser := bmdepl.NewCpiDeploymentParser(f.fs)
	boshManifestParser := bmdepl.NewBoshDeploymentParser(f.fs, f.logger)
	boshDeploymentValidator := bmdeplval.NewBoshDeploymentValidator()
	erbRenderer := bmerbrenderer.NewERBRenderer(f.fs, runner, f.logger)
	jobRenderer := bmtempcomp.NewJobRenderer(erbRenderer, f.fs, f.logger)
	templatesIndex := bmindex.NewFileIndex(f.deploymentConfig.TemplatesIndexPath(), f.fs)
	templatesRepo := bmtempcomp.NewTemplatesRepo(templatesIndex)
	templatesCompiler := bmtempcomp.NewTemplatesCompiler(jobRenderer, compressor, blobstore, templatesRepo, f.fs, f.logger)
	releaseCompiler := bmcomp.NewReleaseCompiler(releasePackagesCompiler, templatesCompiler)
	jobInstaller := bmcpiinstall.NewJobInstaller(
		f.fs,
		packageInstaller,
		blobExtractor,
		templatesRepo,
		f.deploymentConfig.JobsPath(),
		f.deploymentConfig.PackagesPath(),
		eventLogger,
		timeService,
	)
	cloudFactory := bmcloud.NewFactory(f.fs, runner, f.deploymentConfig, f.logger)
	cpiInstaller := bmcpi.NewInstaller(
		f.ui,
		f.fs,
		extractor,
		releaseValidator,
		releaseCompiler,
		jobInstaller,
		cloudFactory,
		f.logger,
	)
	stemcellReader := bmstemcell.NewReader(compressor, f.fs)
	repo := bmstemcell.NewRepo(f.deploymentConfigService)
	stemcellExtractor := bmstemcell.NewExtractor(stemcellReader, f.fs)
	stemcellManagerFactory := bmstemcell.NewManagerFactory(repo, eventLogger)

	agentClientFactory := bmagentclient.NewAgentClientFactory(f.deploymentConfig.DeploymentUUID, 1*time.Second, f.logger)
	blobstoreFactory := bmblobstore.NewBlobstoreFactory(f.fs, f.logger)
	sha1Calculator := bmas.NewSha1Calculator(f.fs)
	applySpecFactory := bmas.NewFactory()

	templatesSpecGenerator := bmas.NewTemplatesSpecGenerator(
		blobstoreFactory,
		compressor,
		jobRenderer,
		f.uuidGenerator,
		sha1Calculator,
		f.fs,
		f.logger,
	)

	vmManagerFactory := bmvm.NewManagerFactory(
		agentClientFactory,
		f.deploymentConfigService,
		applySpecFactory,
		templatesSpecGenerator,
		f.fs,
		f.logger,
	)

	deploymentRecord := bmconfig.NewDeploymentRecord(f.deploymentConfigService, f.logger)
	diskManagerFactory := bmdisk.NewManagerFactory(deploymentRecord, f.logger)
	registryServer := bmregistry.NewServer(f.logger)
	sshTunnelFactory := bmsshtunnel.NewFactory(f.logger)

	deployer := bmdeployer.NewDeployer(
		vmManagerFactory,
		diskManagerFactory,
		sshTunnelFactory,
		registryServer,
		eventLogger,
		f.logger,
	)

	return NewDeployCmd(
		f.ui,
		f.userConfig,
		f.fs,
		deploymentRecord,
		cpiManifestParser,
		boshManifestParser,
		boshDeploymentValidator,
		cpiInstaller,
		stemcellExtractor,
		stemcellManagerFactory,
		deployer,
		eventLogger,
		f.logger,
	), nil
}

func (f *factory) loadDeploymentConfig() error {
	f.deploymentConfigService = bmconfig.NewFileSystemDeploymentConfigService(
		f.userConfig.DeploymentConfigFilePath(),
		f.fs,
		f.logger,
	)
	var err error
	f.deploymentConfig, err = f.deploymentConfigService.Load()
	if err != nil {
		return bosherr.WrapError(err, "Loading deployment config")
	}
	f.deploymentConfig.ContainingDir = f.workspace
	return nil
}
