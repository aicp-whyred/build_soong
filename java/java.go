// Copyright 2015 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package java

// This file contains the module types for compiling Java for Android, and converts the properties
// into the flags and filenames necessary to pass to the compiler.  The final creation of the rules
// is handled in builder.go

import (
	"fmt"
	"strings"

	"github.com/google/blueprint"

	"android/soong"
	"android/soong/android"
	"android/soong/genrule"
)

func init() {
	soong.RegisterModuleType("java_library", JavaLibraryFactory)
	soong.RegisterModuleType("java_library_static", JavaLibraryFactory)
	soong.RegisterModuleType("java_library_host", JavaLibraryHostFactory)
	soong.RegisterModuleType("java_binary", JavaBinaryFactory)
	soong.RegisterModuleType("java_binary_host", JavaBinaryHostFactory)
	soong.RegisterModuleType("prebuilt_java_library", JavaPrebuiltFactory)
	soong.RegisterModuleType("prebuilt_sdk", SdkPrebuiltFactory)
	soong.RegisterModuleType("android_app", AndroidAppFactory)

	soong.RegisterSingletonType("logtags", LogtagsSingleton)
}

// TODO:
// Autogenerated files:
//  Proto
//  Renderscript
// Post-jar passes:
//  Proguard
//  Emma
//  Jarjar
//  Dex
// Rmtypedefs
// Jack
// DroidDoc
// Findbugs

type javaBaseProperties struct {
	// list of source files used to compile the Java module.  May be .java, .logtags, .proto,
	// or .aidl files.
	Srcs []string `android:"arch_variant"`

	// list of source files that should not be used to build the Java module.
	// This is most useful in the arch/multilib variants to remove non-common files
	Exclude_srcs []string `android:"arch_variant"`

	// list of directories containing Java resources
	Java_resource_dirs []string `android:"arch_variant"`

	// list of directories that should be excluded from java_resource_dirs
	Exclude_java_resource_dirs []string `android:"arch_variant"`

	// don't build against the default libraries (core-libart, core-junit,
	// ext, and framework for device targets)
	No_standard_libraries bool

	// list of module-specific flags that will be used for javac compiles
	Javacflags []string `android:"arch_variant"`

	// list of module-specific flags that will be used for jack compiles
	Jack_flags []string `android:"arch_variant"`

	// list of module-specific flags that will be used for dex compiles
	Dxflags []string `android:"arch_variant"`

	// list of of java libraries that will be in the classpath
	Java_libs []string `android:"arch_variant"`

	// list of java libraries that will be compiled into the resulting jar
	Java_static_libs []string `android:"arch_variant"`

	// manifest file to be included in resulting jar
	Manifest *string

	// if not blank, set to the version of the sdk to compile against
	Sdk_version string

	// Set for device java libraries, and for host versions of device java libraries
	// built for testing
	Dex bool `blueprint:"mutated"`

	// if not blank, run jarjar using the specified rules file
	Jarjar_rules *string

	// directories to pass to aidl tool
	Aidl_includes []string

	// directories that should be added as include directories
	// for any aidl sources of modules that depend on this module
	Export_aidl_include_dirs []string
}

// javaBase contains the properties and members used by all java module types, and implements
// the blueprint.Module interface.
type javaBase struct {
	android.ModuleBase
	module JavaModuleType

	properties javaBaseProperties

	// output file suitable for inserting into the classpath of another compile
	classpathFile android.Path

	// output file suitable for installing or running
	outputFile android.Path

	// jarSpecs suitable for inserting classes from a static library into another jar
	classJarSpecs []jarSpec

	// jarSpecs suitable for inserting resources from a static library into another jar
	resourceJarSpecs []jarSpec

	exportAidlIncludeDirs android.Paths

	logtagsSrcs android.Paths

	// filelists of extra source files that should be included in the javac command line,
	// for example R.java generated by aapt for android apps
	ExtraSrcLists android.Paths

	// installed file for binary dependency
	installFile android.Path
}

type AndroidJavaModuleContext android.BaseContext

type JavaModuleType interface {
	GenerateJavaBuildActions(ctx android.ModuleContext)
	JavaDependencies(ctx AndroidJavaModuleContext) []string
}

type JavaDependency interface {
	ClasspathFile() android.Path
	ClassJarSpecs() []jarSpec
	ResourceJarSpecs() []jarSpec
	AidlIncludeDirs() android.Paths
}

func NewJavaBase(base *javaBase, module JavaModuleType, hod android.HostOrDeviceSupported,
	props ...interface{}) (blueprint.Module, []interface{}) {

	base.module = module

	props = append(props, &base.properties)

	return android.InitAndroidArchModule(base, hod, android.MultilibCommon, props...)
}

func (j *javaBase) BootClasspath(ctx android.BaseContext) string {
	if ctx.Device() {
		if j.properties.Sdk_version == "" {
			return "core-libart"
		} else if j.properties.Sdk_version == "current" {
			// TODO: !TARGET_BUILD_APPS
			// TODO: export preprocessed framework.aidl from android_stubs_current
			return "android_stubs_current"
		} else if j.properties.Sdk_version == "system_current" {
			return "android_system_stubs_current"
		} else {
			return "sdk_v" + j.properties.Sdk_version
		}
	} else {
		if j.properties.Dex {
			return "core-libart"
		} else {
			return ""
		}
	}
}

var defaultJavaLibraries = []string{"core-libart", "core-junit", "ext", "framework"}

func javaDepsMutator(ctx android.BottomUpMutatorContext) {
	if j, ok := ctx.Module().(JavaModuleType); ok {
		ctx.AddDependency(ctx.Module(), nil, j.JavaDependencies(ctx)...)
	}
}

func (j *javaBase) JavaDependencies(ctx AndroidJavaModuleContext) []string {
	var deps []string

	if !j.properties.No_standard_libraries {
		bootClasspath := j.BootClasspath(ctx)
		if bootClasspath != "" {
			deps = append(deps, bootClasspath)
		}
		if ctx.Device() && j.properties.Sdk_version == "" {
			deps = append(deps, defaultJavaLibraries...)
		}
	}
	deps = append(deps, j.properties.Java_libs...)
	deps = append(deps, j.properties.Java_static_libs...)

	return deps
}

func (j *javaBase) aidlFlags(ctx android.ModuleContext, aidlPreprocess android.OptionalPath,
	aidlIncludeDirs android.Paths) []string {

	localAidlIncludes := android.PathsForModuleSrc(ctx, j.properties.Aidl_includes)

	var flags []string
	if aidlPreprocess.Valid() {
		flags = append(flags, "-p"+aidlPreprocess.String())
	} else {
		flags = append(flags, android.JoinWithPrefix(aidlIncludeDirs.Strings(), "-I"))
	}

	flags = append(flags, android.JoinWithPrefix(j.exportAidlIncludeDirs.Strings(), "-I"))
	flags = append(flags, android.JoinWithPrefix(localAidlIncludes.Strings(), "-I"))
	flags = append(flags, "-I"+android.PathForModuleSrc(ctx).String())
	flags = append(flags, "-I"+android.PathForModuleSrc(ctx, "src").String())

	return flags
}

func (j *javaBase) collectDeps(ctx android.ModuleContext) (classpath android.Paths,
	bootClasspath android.OptionalPath, classJarSpecs, resourceJarSpecs []jarSpec, aidlPreprocess android.OptionalPath,
	aidlIncludeDirs android.Paths, srcFileLists android.Paths) {

	ctx.VisitDirectDeps(func(module blueprint.Module) {
		otherName := ctx.OtherModuleName(module)
		if javaDep, ok := module.(JavaDependency); ok {
			if otherName == j.BootClasspath(ctx) {
				bootClasspath = android.OptionalPathForPath(javaDep.ClasspathFile())
			} else if inList(otherName, defaultJavaLibraries) {
				classpath = append(classpath, javaDep.ClasspathFile())
			} else if inList(otherName, j.properties.Java_libs) {
				classpath = append(classpath, javaDep.ClasspathFile())
			} else if inList(otherName, j.properties.Java_static_libs) {
				classpath = append(classpath, javaDep.ClasspathFile())
				classJarSpecs = append(classJarSpecs, javaDep.ClassJarSpecs()...)
				resourceJarSpecs = append(resourceJarSpecs, javaDep.ResourceJarSpecs()...)
			} else if otherName == "framework-res" {
				if ctx.ModuleName() == "framework" {
					// framework.jar has a one-off dependency on the R.java and Manifest.java files
					// generated by framework-res.apk
					srcFileLists = append(srcFileLists, module.(*javaBase).module.(*AndroidApp).aaptJavaFileList)
				}
			} else {
				panic(fmt.Errorf("unknown dependency %q for %q", otherName, ctx.ModuleName()))
			}
			aidlIncludeDirs = append(aidlIncludeDirs, javaDep.AidlIncludeDirs()...)
			if sdkDep, ok := module.(sdkDependency); ok {
				if sdkDep.AidlPreprocessed().Valid() {
					if aidlPreprocess.Valid() {
						ctx.ModuleErrorf("multiple dependencies with preprocessed aidls:\n %q\n %q",
							aidlPreprocess, sdkDep.AidlPreprocessed())
					} else {
						aidlPreprocess = sdkDep.AidlPreprocessed()
					}
				}
			}
		}
	})

	return classpath, bootClasspath, classJarSpecs, resourceJarSpecs, aidlPreprocess,
		aidlIncludeDirs, srcFileLists
}

func (j *javaBase) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.module.GenerateJavaBuildActions(ctx)
}

func (j *javaBase) GenerateJavaBuildActions(ctx android.ModuleContext) {

	j.exportAidlIncludeDirs = android.PathsForModuleSrc(ctx, j.properties.Export_aidl_include_dirs)

	classpath, bootClasspath, classJarSpecs, resourceJarSpecs, aidlPreprocess,
		aidlIncludeDirs, srcFileLists := j.collectDeps(ctx)

	var flags javaBuilderFlags

	javacFlags := j.properties.Javacflags
	if len(javacFlags) > 0 {
		ctx.Variable(pctx, "javacFlags", strings.Join(javacFlags, " "))
		flags.javacFlags = "$javacFlags"
	}

	aidlFlags := j.aidlFlags(ctx, aidlPreprocess, aidlIncludeDirs)
	if len(aidlFlags) > 0 {
		ctx.Variable(pctx, "aidlFlags", strings.Join(aidlFlags, " "))
		flags.aidlFlags = "$aidlFlags"
	}

	var javacDeps android.Paths

	if bootClasspath.Valid() {
		flags.bootClasspath = "-bootclasspath " + bootClasspath.String()
		javacDeps = append(javacDeps, bootClasspath.Path())
	}

	if len(classpath) > 0 {
		flags.classpath = "-classpath " + strings.Join(classpath.Strings(), ":")
		javacDeps = append(javacDeps, classpath...)
	}

	srcFiles := ctx.ExpandSources(j.properties.Srcs, j.properties.Exclude_srcs)

	srcFiles = j.genSources(ctx, srcFiles, flags)

	ctx.VisitDirectDeps(func(module blueprint.Module) {
		if gen, ok := module.(genrule.SourceFileGenerator); ok {
			srcFiles = append(srcFiles, gen.GeneratedSourceFiles()...)
		}
	})

	srcFileLists = append(srcFileLists, j.ExtraSrcLists...)

	if len(srcFiles) > 0 {
		// Compile java sources into .class files
		classes := TransformJavaToClasses(ctx, srcFiles, srcFileLists, flags, javacDeps)
		if ctx.Failed() {
			return
		}

		classJarSpecs = append([]jarSpec{classes}, classJarSpecs...)
	}

	resourceJarSpecs = append(ResourceDirsToJarSpecs(ctx, j.properties.Java_resource_dirs, j.properties.Exclude_java_resource_dirs),
		resourceJarSpecs...)

	manifest := android.OptionalPathForModuleSrc(ctx, j.properties.Manifest)

	allJarSpecs := append([]jarSpec(nil), classJarSpecs...)
	allJarSpecs = append(allJarSpecs, resourceJarSpecs...)

	// Combine classes + resources into classes-full-debug.jar
	outputFile := TransformClassesToJar(ctx, allJarSpecs, manifest)
	if ctx.Failed() {
		return
	}

	if j.properties.Jarjar_rules != nil {
		jarjar_rules := android.PathForModuleSrc(ctx, *j.properties.Jarjar_rules)
		// Transform classes-full-debug.jar into classes-jarjar.jar
		outputFile = TransformJarJar(ctx, outputFile, jarjar_rules)
		if ctx.Failed() {
			return
		}

		classes, _ := TransformPrebuiltJarToClasses(ctx, outputFile)
		classJarSpecs = []jarSpec{classes}
	}

	j.resourceJarSpecs = resourceJarSpecs
	j.classJarSpecs = classJarSpecs
	j.classpathFile = outputFile

	if j.properties.Dex && len(srcFiles) > 0 {
		dxFlags := j.properties.Dxflags
		if false /* emma enabled */ {
			// If you instrument class files that have local variable debug information in
			// them emma does not correctly maintain the local variable table.
			// This will cause an error when you try to convert the class files for Android.
			// The workaround here is to build different dex file here based on emma switch
			// then later copy into classes.dex. When emma is on, dx is run with --no-locals
			// option to remove local variable information
			dxFlags = append(dxFlags, "--no-locals")
		}

		if ctx.AConfig().Getenv("NO_OPTIMIZE_DX") != "" {
			dxFlags = append(dxFlags, "--no-optimize")
		}

		if ctx.AConfig().Getenv("GENERATE_DEX_DEBUG") != "" {
			dxFlags = append(dxFlags,
				"--debug",
				"--verbose",
				"--dump-to="+android.PathForModuleOut(ctx, "classes.lst").String(),
				"--dump-width=1000")
		}

		flags.dxFlags = strings.Join(dxFlags, " ")

		// Compile classes.jar into classes.dex
		dexJarSpec := TransformClassesJarToDex(ctx, outputFile, flags)
		if ctx.Failed() {
			return
		}

		// Combine classes.dex + resources into javalib.jar
		outputFile = TransformDexToJavaLib(ctx, resourceJarSpecs, dexJarSpec)
	}
	ctx.CheckbuildFile(outputFile)
	j.outputFile = outputFile
}

var _ JavaDependency = (*JavaLibrary)(nil)

func (j *javaBase) ClasspathFile() android.Path {
	return j.classpathFile
}

func (j *javaBase) ClassJarSpecs() []jarSpec {
	return j.classJarSpecs
}

func (j *javaBase) ResourceJarSpecs() []jarSpec {
	return j.resourceJarSpecs
}

func (j *javaBase) AidlIncludeDirs() android.Paths {
	return j.exportAidlIncludeDirs
}

var _ logtagsProducer = (*javaBase)(nil)

func (j *javaBase) logtags() android.Paths {
	return j.logtagsSrcs
}

//
// Java libraries (.jar file)
//

type JavaLibrary struct {
	javaBase
}

func (j *JavaLibrary) GenerateJavaBuildActions(ctx android.ModuleContext) {
	j.javaBase.GenerateJavaBuildActions(ctx)

	j.installFile = ctx.InstallFileName(android.PathForModuleInstall(ctx, "framework"), ctx.ModuleName()+".jar", j.outputFile)
}

func JavaLibraryFactory() (blueprint.Module, []interface{}) {
	module := &JavaLibrary{}

	module.properties.Dex = true

	return NewJavaBase(&module.javaBase, module, android.HostAndDeviceSupported)
}

func JavaLibraryHostFactory() (blueprint.Module, []interface{}) {
	module := &JavaLibrary{}

	return NewJavaBase(&module.javaBase, module, android.HostSupported)
}

//
// Java Binaries (.jar file plus wrapper script)
//

type javaBinaryProperties struct {
	// installable script to execute the resulting jar
	Wrapper string
}

type JavaBinary struct {
	JavaLibrary

	binaryProperties javaBinaryProperties
}

func (j *JavaBinary) GenerateJavaBuildActions(ctx android.ModuleContext) {
	j.JavaLibrary.GenerateJavaBuildActions(ctx)

	// Depend on the installed jar (j.installFile) so that the wrapper doesn't get executed by
	// another build rule before the jar has been installed.
	ctx.InstallFile(android.PathForModuleInstall(ctx, "bin"), android.PathForModuleSrc(ctx, j.binaryProperties.Wrapper),
		j.installFile)
}

func JavaBinaryFactory() (blueprint.Module, []interface{}) {
	module := &JavaBinary{}

	module.properties.Dex = true

	return NewJavaBase(&module.javaBase, module, android.HostAndDeviceSupported, &module.binaryProperties)
}

func JavaBinaryHostFactory() (blueprint.Module, []interface{}) {
	module := &JavaBinary{}

	return NewJavaBase(&module.javaBase, module, android.HostSupported, &module.binaryProperties)
}

//
// Java prebuilts
//

type javaPrebuiltProperties struct {
	Srcs []string
}

type JavaPrebuilt struct {
	android.ModuleBase

	properties javaPrebuiltProperties

	classpathFile                   android.Path
	classJarSpecs, resourceJarSpecs []jarSpec
}

func (j *JavaPrebuilt) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if len(j.properties.Srcs) != 1 {
		ctx.ModuleErrorf("expected exactly one jar in srcs")
		return
	}
	prebuilt := android.PathForModuleSrc(ctx, j.properties.Srcs[0])

	classJarSpec, resourceJarSpec := TransformPrebuiltJarToClasses(ctx, prebuilt)

	j.classpathFile = prebuilt
	j.classJarSpecs = []jarSpec{classJarSpec}
	j.resourceJarSpecs = []jarSpec{resourceJarSpec}
	ctx.InstallFileName(android.PathForModuleInstall(ctx, "framework"), ctx.ModuleName()+".jar", j.classpathFile)
}

var _ JavaDependency = (*JavaPrebuilt)(nil)

func (j *JavaPrebuilt) ClasspathFile() android.Path {
	return j.classpathFile
}

func (j *JavaPrebuilt) ClassJarSpecs() []jarSpec {
	return j.classJarSpecs
}

func (j *JavaPrebuilt) ResourceJarSpecs() []jarSpec {
	return j.resourceJarSpecs
}

func (j *JavaPrebuilt) AidlIncludeDirs() android.Paths {
	return nil
}

func JavaPrebuiltFactory() (blueprint.Module, []interface{}) {
	module := &JavaPrebuilt{}

	return android.InitAndroidArchModule(module, android.HostAndDeviceSupported,
		android.MultilibCommon, &module.properties)
}

//
// SDK java prebuilts (.jar containing resources plus framework.aidl)
//

type sdkDependency interface {
	JavaDependency
	AidlPreprocessed() android.OptionalPath
}

var _ sdkDependency = (*sdkPrebuilt)(nil)

type sdkPrebuiltProperties struct {
	Aidl_preprocessed *string
}

type sdkPrebuilt struct {
	JavaPrebuilt

	sdkProperties sdkPrebuiltProperties

	aidlPreprocessed android.OptionalPath
}

func (j *sdkPrebuilt) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.JavaPrebuilt.GenerateAndroidBuildActions(ctx)

	j.aidlPreprocessed = android.OptionalPathForModuleSrc(ctx, j.sdkProperties.Aidl_preprocessed)
}

func (j *sdkPrebuilt) AidlPreprocessed() android.OptionalPath {
	return j.aidlPreprocessed
}

func SdkPrebuiltFactory() (blueprint.Module, []interface{}) {
	module := &sdkPrebuilt{}

	return android.InitAndroidArchModule(module, android.HostAndDeviceSupported,
		android.MultilibCommon, &module.properties, &module.sdkProperties)
}

func inList(s string, l []string) bool {
	for _, e := range l {
		if e == s {
			return true
		}
	}
	return false
}
