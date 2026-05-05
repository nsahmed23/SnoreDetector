//! Regenerates `include/snoreguard_core.h` from the Rust FFI surface.
//!
//! Runs on every `cargo build`. The generated header is checked into
//! the repository so Swift consumers don't need cargo installed to
//! build the iOS app.

use std::env;
use std::path::PathBuf;

fn main() {
    let crate_dir = env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR not set");
    let out_path = PathBuf::from(&crate_dir).join("include").join("snoreguard_core.h");

    if let Some(parent) = out_path.parent() {
        std::fs::create_dir_all(parent).expect("create include dir");
    }

    match cbindgen::Builder::new()
        .with_crate(&crate_dir)
        .with_config(cbindgen::Config::from_file(format!("{crate_dir}/cbindgen.toml")).unwrap())
        .generate()
    {
        Ok(b) => {
            b.write_to_file(&out_path);
        }
        Err(e) => {
            // Emit a warning but don't fail the build — the checked-in
            // header is the source of truth.
            println!("cargo:warning=cbindgen failed: {e}");
        }
    }

    println!("cargo:rerun-if-changed=src/ffi.rs");
    println!("cargo:rerun-if-changed=src/lib.rs");
    println!("cargo:rerun-if-changed=src/detector.rs");
    println!("cargo:rerun-if-changed=cbindgen.toml");
}
