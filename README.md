# PumpCost

PumpCost is an automated, zero-cost fuel price tracker for filling stations across the United Kingdom. It downloads forecourt pricing data from the UK Fuel Finder Scheme, splits the records into lightweight regional JSON files by UK postcode district, and publishes an interactive web interface directly to GitHub Pages.

## Prerequisites

Before setting up or running PumpCost, ensure you have the following installed on your computer:

- Go (version 1.22 or higher)
- Git
- Make (optional, for running convenience commands)
- A modern web browser such as Chrome, Firefox, Safari, or Edge
- A GitHub account for publishing the project to GitHub Pages

## How to Register for API Credentials

PumpCost queries official UK forecourt prices using the UK Fuel Finder Scheme API operated by the Department for Energy Security and Net Zero and the Competition and Markets Authority.

Follow these steps to obtain API credentials:

1. Visit the official UK Fuel Finder developer portal at https://www.developer.fuel-finder.service.gov.uk/
2. Register for a developer account using your email address and verify your account.
3. Sign in and navigate to the Applications or API Keys section in the developer dashboard.
4. Click Create New Application and enter a title for your project (such as "PumpCost Tracker").
5. Select OAuth 2.0 Client Credentials as the authentication type.
6. Copy your generated Client ID and Client Secret. Store these values securely. You will use them as environment variables.

If you want to run the project without registering first, PumpCost includes a built-in sample data generator so you can test the pipeline and user interface immediately.

## How to Test and Run the Project Locally

You can test and run both the data processing pipeline and the web frontend on your local machine.

### Step 1: Clone the repository

```bash
git clone https://github.com/smford/pumpcost.git
cd pumpcost
```

### Step 2: Run automated tests

Run the unit tests to verify the postcode sharding logic, OAuth client handling, and HTTP error recovery:

```bash
make test
```

Or using the standard Go test command:

```bash
go test -v ./...
```

### Step 3: Run the data pipeline

To run the pipeline using built-in mock data (no credentials required):

```bash
make run-pipeline
```

To run the pipeline with live UK Fuel Finder credentials:

```bash
export FUEL_FINDER_CLIENT_ID="your_client_id_here"
export FUEL_FINDER_CLIENT_SECRET="your_client_secret_here"
make run-pipeline
```

The pipeline writes minified JSON files for each UK postcode district into the `data/outcodes` directory, along with a master index file at `data/outcodes/index.json`.

### Step 4: Start the local preview server

Start the built-in development web server:

```bash
make serve-local
```

Open your browser and navigate to:

```
http://localhost:8080
```

The page will load, detect your nearest postcode district via browser geolocation (or let you search any district such as SS1, SW1A, BS1, or EH1), and display nearby stations sorted by lowest fuel price.

## How to Deploy to GitHub Pages

PumpCost is designed to run entirely on free GitHub infrastructure without requiring servers or paid database instances.

Follow these steps to deploy:

### Step 1: Push your code to GitHub

Create a new repository on GitHub named `pumpcost` and push your code to the `main` branch:

```bash
git remote add origin https://github.com/smford/pumpcost.git
git branch -M main
git push -u origin main
```

### Step 2: Add your API credentials as repository secrets

1. Open your repository on GitHub.
2. Go to Settings > Secrets and variables > Actions.
3. Click New repository secret.
4. Add a secret named `FUEL_FINDER_CLIENT_ID` with your API Client ID.
5. Add a second secret named `FUEL_FINDER_CLIENT_SECRET` with your API Client Secret.

### Step 3: Configure GitHub Pages source

1. In your repository settings, go to the Pages tab on the left sidebar.
2. Under Build and deployment > Source, select "GitHub Actions".

### Step 4: Run the automated workflow

1. Go to the Actions tab in your GitHub repository.
2. Select the "Update Fuel Data and Deploy Pages" workflow in the left list.
3. Click Run workflow > Run workflow.

The workflow will run the Go pipeline, generate the sharded data files, package the static assets, and deploy the site directly to GitHub Pages. Once complete, GitHub Pages will provide a live URL where your tracker is publicly accessible.

The workflow is configured to run automatically every 30 minutes via GitHub Actions cron, keeping fuel prices up to date without manual intervention.
