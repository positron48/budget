# Budget - Multi-user Income and Expense Tracking

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![Next.js](https://img.shields.io/badge/Next.js-14+-000000?style=for-the-badge&logo=next.js&logoColor=white)](https://nextjs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Docker](https://img.shields.io/badge/Docker-✓-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://www.docker.com/)

[![CI](https://img.shields.io/badge/CI-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/ci.yml)
[![Security Audit](https://img.shields.io/badge/Security%20Audit-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/security.yml)
[![Generate Stubs](https://img.shields.io/badge/Generate%20Protobuf%20Stubs-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/generate-stubs.yml)
[![License](https://img.shields.io/badge/License-CC%20BY--NC%204.0-yellow?style=for-the-badge)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-Welcome-brightgreen?style=for-the-badge)](http://makeapullrequest.com)
[![Go Report Card](https://img.shields.io/badge/Go%20Report%20Card-A+-brightgreen?style=for-the-badge&logo=go)](https://goreportcard.com/report/github.com/positron48/budget)
[![Codecov](https://img.shields.io/badge/coverage-80%25-brightgreen?style=for-the-badge)](https://codecov.io/gh/positron48/budget)
[![Dependabot](https://img.shields.io/badge/dependabot-enabled-025e8c?style=for-the-badge&logo=dependabot)](https://dependabot.com/)

<div align="center">

[![Contributors](https://img.shields.io/github/contributors/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/graphs/contributors)
[![Last Commit](https://img.shields.io/github/last-commit/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/commits)
[![Release](https://img.shields.io/github/release/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/releases)

</div>

[English](README_EN.md) | [Русский](README.md)

> Modern web application for personal finance tracking with multi-user support, data import/export, and beautiful interface.

## Quick Start

### Prerequisites
- Docker and Docker Compose
- Git

### Installation and Setup
```bash
# Clone repository
git clone https://github.com/positron48/budget
cd budget

# Start entire environment
make up

# Check status
make logs
```

### Access to Services
- **Frontend**: http://localhost:3030
- **Backend gRPC**: localhost:8080
- **Grafana**: http://localhost:3002
- **Prometheus**: http://localhost:9090

### First Steps
1. Open http://localhost:3030
2. Register with email/password
3. Create your first organization
4. Start adding categories and transactions

## Key Features

### Transaction Management
- ✅ CRUD operations for transactions (income/expense)
- ✅ Categorization with i18n support
- ✅ Filtering and search with quick filters
- ✅ Pagination and sorting
- ✅ CSV export with filters

### Analytics and Reports
- ✅ Monthly reports by categories
- ✅ Data visualization with charts
- ✅ Period comparison
- ✅ Income and expense statistics

### Data Import/Export
- ✅ CSV file import with mapping configuration
- ✅ Automatic encoding detection
- ✅ Data preview
- ✅ Export with all filters applied

### Multi-user Support
- ✅ Multi-tenant architecture
- ✅ Roles: Owner, Admin, Member
- ✅ Organization management
- ✅ Data isolation between accounts

### Internationalization
- ✅ Support for Russian and English languages
- ✅ Localized categories
- ✅ Automatic language switching

## Architecture

```mermaid
flowchart TB
  subgraph Client
    Web["Web App (Next.js + TS)"]
    TG["Telegram Bot (Go)"]
  end

  subgraph Edge
    Envoy["Envoy Proxy<br/>gRPC-Web → gRPC"]
  end

  subgraph Core
    Budgetd["budgetd (Go)<br/>Hexagonal Architecture<br/>– gRPC API<br/>– Domain Services<br/>– Repository Pattern"]
  end

  subgraph Infra
    PG["PostgreSQL 15+"]
    MQ["NATS/RabbitMQ"]
    S3["S3 Storage"]
    OTEL["OpenTelemetry"]
  end

  Web -->|gRPC-Web| Envoy -->|gRPC| Budgetd
  TG -->|gRPC| Budgetd
  Budgetd <-->|SQL| PG
  Budgetd -->|events| MQ
  Budgetd -->|files| S3
  Budgetd --> OTEL
```

## Technology Stack

### Backend
- **Go 1.23+** - main server language
- **gRPC** - API protocol with protobuf
- **PostgreSQL 15+** - main database
- **Argon2id** - password hashing
- **JWT** - authentication with refresh tokens

### Frontend
- **Next.js 14** - React framework
- **TypeScript** - typed JavaScript
- **Tailwind CSS** - utility CSS framework
- **TanStack Query** - state management
- **Connect-Web** - gRPC client for browser

### DevOps
- **Docker** - containerization
- **Prometheus + Grafana** - monitoring
- **OpenTelemetry** - tracing and metrics
- **GitHub Actions** - CI/CD

## Deployment

### Local Development
```bash
# Start entire environment
make up

# Stop
make down

# Checks
make check

# Generate protobuf
make proto

# Database migrations
make migrate-up
```

### Production
```bash
# Build and start
docker-compose -f docker-compose.yml up -d

# Monitoring
docker-compose -f docker-compose.yml logs -f
```

## Project Structure

```
budget/
├── 📁 cmd/budgetd/              # Backend entry point
├── 📁 internal/                 # Backend business logic
│   ├── 📁 domain/              # Domain entities
│   ├── 📁 usecase/             # Application services
│   ├── 📁 adapter/             # Infrastructure adapters
│   └── 📁 pkg/                 # Shared utilities
├── 📁 web/                     # Frontend (Next.js)
│   ├── 📁 app/                 # Next.js App Router
│   ├── 📁 components/          # React components
│   ├── 📁 lib/                 # Utilities and API clients
│   └── 📁 i18n/                # Internationalization
├── 📁 proto/                   # gRPC schemas
├── 📁 migrations/              # Database migrations
├── 📁 deploy/                  # Docker and monitoring
└── 📁 docs/                    # Documentation
```

## Main Commands

| Command | Description |
|---------|-------------|
| `make up` | Start entire environment |
| `make down` | Stop all services |
| `make check` | Backend + frontend checks |
| `make proto` | Generate protobuf code |
| `make migrate-up` | Apply database migrations |
| `make test` | Run tests |
| `make logs` | View logs |

## Roadmap

### In Development
- **Telegram bot** for quick transaction addition
- **Bank integrations** for automatic import
- **Budget planning** and financial goals

### Planned
- **Mobile application** (React Native)
- **E2E tests** with Playwright
- **Performance optimizations**
- **PWA functionality**
- **Offline mode**
- **Push notifications**

## Contributing

We welcome contributions to the project! Please read our [contributing guidelines](CONTRIBUTING.md).

### How to help:
1. **Report a bug** - create an issue
2. **Suggest an idea** - create a feature request
3. **Fix a bug** - create a pull request
4. **Improve documentation** - edit README

## License

This project is licensed under the Creative Commons Attribution-NonCommercial 4.0 International License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [Go](https://golang.org/) - for the excellent programming language
- [Next.js](https://nextjs.org/) - for the modern React framework
- [Tailwind CSS](https://tailwindcss.com/) - for the utility CSS
- [gRPC](https://grpc.io/) - for the efficient API protocol

---

**⭐ If you like the project, give it a star!**
