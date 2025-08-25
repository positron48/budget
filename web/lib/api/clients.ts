// @ts-nocheck
import type { Transport } from "@connectrpc/connect";
import { createPromiseClient } from "@connectrpc/connect";

import { AuthService } from "../../proto/budget/v1/auth_connect";
import { UserService } from "../../proto/budget/v1/user_connect";
import { TenantService } from "../../proto/budget/v1/tenant_connect";
import { CategoryService } from "../../proto/budget/v1/category_connect";
import { TransactionService } from "../../proto/budget/v1/transaction_connect";
import { ReportService } from "../../proto/budget/v1/report_connect";
import { FxService } from "../../proto/budget/v1/fx_connect";
import { ImportService } from "../../proto/budget/v1/import_connect";

export function createClients(transport: Transport) {
  return {
    auth: createPromiseClient(AuthService as any, transport),
    user: createPromiseClient(UserService as any, transport),
    tenant: createPromiseClient(TenantService as any, transport),
    category: createPromiseClient(CategoryService as any, transport),
    transaction: createPromiseClient(TransactionService as any, transport),
    report: createPromiseClient(ReportService as any, transport),
    fx: createPromiseClient(FxService as any, transport),
    importSvc: createPromiseClient(ImportService as any, transport),
  } as const;
}


