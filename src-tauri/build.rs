fn main() {
    tauri_build::build();
    link_manifest_into_tests();
}

/// Links tauri-build's Windows resource (application manifest) into TEST
/// binaries as well.
///
/// `tauri_build::build()` emits `cargo:rustc-link-arg-bins=<out>/resource.lib`,
/// so only the shipped binary carries the embedded manifest with the
/// Common-Controls v6 side-by-side dependency. Any test binary that pulls
/// Tauri's app machinery (e.g. the `tauri::test` mock runtime) also links
/// native GUI-stack objects with comctl32 v6-only imports
/// (`TaskDialogIndirect`), and without the manifest those binaries fail to
/// load with STATUS_ENTRYPOINT_NOT_FOUND (0xc0000139) before any test runs.
///
/// Reusing the very same `resource.lib` inside the build script's `OUT_DIR`
/// keeps the manifest bit-identical to the shipped app's. Failures are
/// non-fatal: if tauri-build ever renames the artifact, GUI-free test
/// binaries keep working and only GUI-linked ones are affected.
fn link_manifest_into_tests() {
    let Some(out_dir) = std::env::var_os("OUT_DIR").map(std::path::PathBuf::from) else {
        return;
    };
    let resource = out_dir.join("resource.lib");
    if resource.is_file() {
        println!("cargo:rustc-link-arg-tests={}", resource.display());
    } else {
        println!(
            "cargo:warning=FloatTranslate test manifest resource not found at {}; \
             GUI-linked test binaries may fail to load on Windows",
            resource.display()
        );
    }
}
