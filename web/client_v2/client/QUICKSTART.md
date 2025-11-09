# Quick Start Guide

## Start the Client

```bash
cd /home/qing/work/meltica/web/client
pnpm dev
```

The application will be available at **http://localhost:3000**

## Prerequisites

Ensure the Meltica gateway is running at **http://localhost:8880**

## Test the Connection

Once the dev server is running, you can:

1. Visit http://localhost:3000 for the dashboard
2. Navigate to http://localhost:3000/strategies to view available strategies
3. Navigate to http://localhost:3000/providers to see exchange providers
4. Navigate to http://localhost:3000/instances to manage strategy instances
5. Navigate to http://localhost:3000/risk to configure risk limits

## Create Your First Instance

1. Go to **Strategy Instances** page
2. Click **Create Instance**
3. Fill in the form:
   - **Instance ID**: e.g., `my-first-strategy`
   - **Strategy**: Select from dropdown (e.g., `Logging`)
   - **Provider**: Select `binance-spot`
   - **Symbols**: e.g., `BTC-USDT, ETH-USDT`
   - **Configuration**: `{"dry_run": true, "logger_prefix": "[Test] "}`
4. Click **Create & Start**

## Key Features Implemented

✅ Strategy catalog browsing  
✅ Provider monitoring with instrument counts  
✅ Adapter capability viewing  
✅ Full instance lifecycle management (Create, Start, Stop, Delete)  
✅ Risk limits configuration  
✅ Responsive UI with shadcn/ui components  
✅ Real-time API integration with error handling  
