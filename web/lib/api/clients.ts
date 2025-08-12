// @ts-nocheck
import type { Transport } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";

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
    auth: createClient(AuthService as any, transport),
    user: createClient(UserService as any, transport),
    tenant: createClient(TenantService as any, transport),
    category: createClient(CategoryService as any, transport),
    transaction: createClient(TransactionService as any, transport),
    report: createClient(ReportService as any, transport),
    fx: createClient(FxService as any, transport),
    importSvc: createClient(ImportService as any, transport),
  } as const;
}


