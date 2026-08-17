//! Procedural macros for Telemetry.
//!
//! This crate provides derive macros and attribute macros for the Telemetry library.
//!
//! # Derive Macros
//!
//! - `Metrics`: Automatically implement the `RecordMetrics` trait for structs
//!
//! # Attribute Macros
//!
//! - `trace`: Automatically instrument functions with tracing spans

use proc_macro::TokenStream;
use quote::quote;
use syn::{Data, DeriveInput, Expr, ExprLit, Fields, ItemFn, Lit, parse_macro_input};

/// Derives `RecordMetrics` for a struct with named fields annotated using `#[metric(...)]`.
///
/// Each annotated field must specify a metric `name` and exactly one metric type:
/// `counter`, `histogram`, or `gauge`. An optional `description` may also be provided.
/// Fields without a valid name or metric type are excluded.
///
/// # Examples
///
/// ```ignore
/// use telemetry::derive::Metrics;
///
/// #[derive(Metrics)]
/// struct MyMetrics {
///     #[metric(name = "requests_total", counter, description = "Total requests")]
///     requests: u64,
/// }
///
/// let metrics = MyMetrics { requests: 42 };
/// assert_eq!(metrics.metric_fields()[0].name, "requests_total");
/// ```
///
/// # Panics
///
/// Panics when applied to a non-struct or a struct without named fields.
#[proc_macro_derive(Metrics, attributes(metric))]
pub fn derive_metrics(input: TokenStream) -> TokenStream {
    let input = parse_macro_input!(input as DeriveInput);
    let name = &input.ident;

    let fields = match &input.data {
        Data::Struct(data) => match &data.fields {
            Fields::Named(fields) => &fields.named,
            _ => panic!("Metrics derive only supports named fields"),
        },
        _ => panic!("Metrics derive only supports structs"),
    };

    let metric_fields: Vec<_> = fields
        .iter()
        .filter_map(|field| {
            let field_name = field.ident.as_ref()?;

            // Find #[metric(...)] attribute
            for attr in &field.attrs {
                if !attr.path().is_ident("metric") {
                    continue;
                }

                let mut metric_name: Option<String> = None;
                let mut metric_type: Option<String> = None;
                let mut description: Option<String> = None;

                // Parse the attribute arguments
                let _ = attr.parse_nested_meta(|meta| {
                    if meta.path.is_ident("name") {
                        let value: Expr = meta.value()?.parse()?;
                        if let Expr::Lit(ExprLit {
                            lit: Lit::Str(s), ..
                        }) = value
                        {
                            metric_name = Some(s.value());
                        }
                    } else if meta.path.is_ident("description") {
                        let value: Expr = meta.value()?.parse()?;
                        if let Expr::Lit(ExprLit {
                            lit: Lit::Str(s), ..
                        }) = value
                        {
                            description = Some(s.value());
                        }
                    } else if meta.path.is_ident("counter") {
                        metric_type = Some("counter".to_string());
                    } else if meta.path.is_ident("histogram") {
                        metric_type = Some("histogram".to_string());
                    } else if meta.path.is_ident("gauge") {
                        metric_type = Some("gauge".to_string());
                    }
                    Ok(())
                });

                if let (Some(name), Some(mtype)) = (metric_name, metric_type) {
                    let type_expr = match mtype.as_str() {
                        "counter" => quote! { telemetry::metrics::MetricType::Counter },
                        "histogram" => quote! { telemetry::metrics::MetricType::Histogram },
                        "gauge" => quote! { telemetry::metrics::MetricType::Gauge },
                        _ => continue,
                    };

                    let desc = description.unwrap_or_default();

                    return Some(quote! {
                        telemetry::metrics::MetricField {
                            name: #name.to_string(),
                            metric_type: #type_expr,
                            description: #desc.to_string(),
                            value: self.#field_name as f64,
                        }
                    });
                }
            }
            None
        })
        .collect();

    let expanded = quote! {
        impl telemetry::metrics::RecordMetrics for #name {
            fn metric_fields(&self) -> Vec<telemetry::metrics::MetricField> {
                vec![
                    #(#metric_fields),*
                ]
            }
        }
    };

    TokenStream::from(expanded)
}

/// Instruments a function with a tracing span named after the function.
///
/// Synchronous functions execute within the span, while asynchronous functions
/// carry the span across the returned future.
///
/// # Examples
///
/// ```ignore
/// use telemetry::derive::instrument;
///
/// #[instrument]
/// fn process_request() {
///     // Function body
/// }
///
/// #[instrument]
/// async fn fetch_data() {
///     // Function body
/// }
/// ```
pub fn instrument(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let input = parse_macro_input!(item as ItemFn);

    let fn_name = &input.sig.ident;
    let fn_name_str = fn_name.to_string();
    let fn_vis = &input.vis;
    let fn_sig = &input.sig;
    let fn_block = &input.block;
    let fn_attrs = &input.attrs;

    let is_async = fn_sig.asyncness.is_some();

    let expanded = if is_async {
        quote! {
            #(#fn_attrs)*
            #fn_vis #fn_sig {
                let __telemetry_span = ::telemetry::tracing::reexport::info_span!(#fn_name_str);
                let __telemetry_future = async move #fn_block;
                ::telemetry::tracing::reexport::Instrument::instrument(__telemetry_future, __telemetry_span).await
            }
        }
    } else {
        quote! {
            #(#fn_attrs)*
            #fn_vis #fn_sig {
                let __telemetry_span = ::telemetry::tracing::reexport::info_span!(#fn_name_str);
                let __telemetry_enter = __telemetry_span.enter();
                let __telemetry_result = (|| #fn_block)();
                drop(__telemetry_enter);
                __telemetry_result
            }
        }
    };

    TokenStream::from(expanded)
}

/// Backward-compatible alias for `instrument`.
#[proc_macro_attribute]
pub fn trace(attr: TokenStream, item: TokenStream) -> TokenStream {
    instrument(attr, item)
}
