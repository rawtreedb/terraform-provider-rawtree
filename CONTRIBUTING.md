# Contributing to the Rawtree Terraform Provider

Thank you for your interest in contributing! This document provides guidelines for contributing to this project.

## Getting Started

### Prerequisites

- [Go](https://golang.org/doc/install) >= 1.26
- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- AWS credentials for running acceptance tests
- A Rawtree account for running acceptance tests

### Setting Up Your Development Environment

1. Clone the repository:

   ```sh
   git clone https://github.com/rawtreedb/terraform-provider-rawtree.git
   cd terraform-provider-rawtree
   ```

2. Build the provider:

   ```sh
   make build
   ```

3. Run unit tests:

   ```sh
   make test
   ```

4. Run the linter:

   ```sh
   make lint
   ```

### Using a Local Build with Terraform

Add a `dev_overrides` block to `~/.terraformrc` pointing at your build directory:

```hcl
provider_installation {
  dev_overrides {
    "rawtreedb/rawtree" = "/path/to/terraform-provider-rawtree"
  }
  direct {}
}
```

Then run `make install` and use Terraform normally — it will pick up your local build.

## Making Changes

### Code Structure

```
internal/
├── client/                      # Shared Rawtree API client
├── provider/                    # Provider configuration and setup
├── util/                        # Shared AWS helpers (IAM, S3, naming, tags, …)
└── resources/
    ├── s3_ingestion/            # S3 batch + streaming ingestion
    │   ├── resource.go          # CRUD lifecycle
    │   ├── schema.go            # Resource schema
    │   ├── models.go            # State models
    │   ├── iam.go               # IAM role management
    │   ├── glue.go              # Glue job management
    │   ├── lambda.go            # Lambda function management
    │   ├── eventbridge.go       # EventBridge setup
    │   └── scripts/             # Embedded Python scripts (Glue, Lambda)
    ├── waf_ingestion/           # WAF logs via Kinesis Firehose
    ├── cloudfront_ingestion/    # CloudFront real-time logs via Kinesis + Firehose
    └── supabase_cdc_ingestion/  # Supabase Postgres CDC via ECS Fargate
```

Each resource follows the same pattern: `resource.go` (CRUD), `schema.go` (schema definition), `models.go` (state types), plus AWS service-specific files.

### Adding a New Resource

1. Create a new directory under `internal/resources/`
2. Implement the `resource.Resource` interface using `terraform-plugin-framework`
3. Register the resource in `internal/provider/provider.go`
4. Add documentation in `docs/resources/` and examples in `examples/resources/`
5. Add unit tests and, if possible, acceptance tests

### Running Acceptance Tests

Acceptance tests create real AWS resources and require credentials + a Rawtree API key:

```sh
export RAWTREE_API_KEY=rw_...
export RAWTREE_URL=https://api.us-east-1.aws.rawtree.com
export RAWTREE_ORG=your-org
export RAWTREE_PROJECT=your-project
make testacc
```

See `test/README.md` for per-resource setup details.

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Run `make lint` before submitting (uses `golangci-lint`)
- Use meaningful variable and function names
- Keep functions focused and reasonably sized

## Pull Request Process

1. Fork the repository and create a feature branch from `main`
2. Make your changes with clear, descriptive commits
3. Ensure all checks pass locally:
   ```sh
   make fmt
   make lint
   make test
   ```
4. Update documentation if your change affects the user-facing schema or behavior
5. Open a pull request with a clear description of the change and the motivation behind it

## Reporting Issues

- Use GitHub Issues to report bugs
- Include Terraform version, provider version, and relevant configuration
- Include the full error message and debug output (`TF_LOG=DEBUG`) if applicable

## Security

To report a security vulnerability, please email security@rawtreedb.com instead of opening a public issue.

## License

By contributing, you agree that your contributions will be licensed under the Mozilla Public License v2.0.
