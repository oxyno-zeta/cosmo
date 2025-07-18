import { OverrideChange } from '@wundergraph/cosmo-connect/dist/platform/v1/platform_pb';
import { aliasedTable, and, asc, desc, eq, isNull, sql } from 'drizzle-orm';
import { PostgresJsDatabase } from 'drizzle-orm/postgres-js';
import { PlainMessage } from '@bufbuild/protobuf';
import { DBSchemaChangeType } from '../../db/models.js';
import * as schema from '../../db/schema.js';
import { federatedGraphClients, federatedGraphPersistedOperations, users } from '../../db/schema.js';
import {
  ClientDTO,
  PersistedOperationDTO,
  PersistedOperationWithClientDTO,
  SchemaChangeType,
  SchemaCheckDetailsDTO,
  UpdatedPersistedOperation,
} from '../../types/index.js';
import { SchemaCheckRepository } from './SchemaCheckRepository.js';

type ChangeOverride = IgnoreAllOverride & {
  changeType: DBSchemaChangeType;
  path: string | null;
};

type IgnoreAllOverride = {
  createdAt: string;
  id: string;
  name: string;
  createdBy: string | null;
  namespaceId: string;
  hash: string;
};

export class OperationsRepository {
  constructor(
    private db: PostgresJsDatabase<typeof schema>,
    private federatedGraphId: string,
  ) {}

  public async updatePersistedOperations(clientId: string, userId: string, operations: UpdatedPersistedOperation[]) {
    const now = new Date();
    const inserts: (typeof federatedGraphPersistedOperations.$inferInsert)[] = operations.map((operation) => {
      return {
        federatedGraphId: this.federatedGraphId,
        clientId,
        operationId: operation.operationId,
        hash: operation.hash,
        filePath: operation.filePath,
        createdAt: now,
        updatedAt: now,
        createdById: userId,
        operationContent: operation.contents,
        operationNames: operation.operationNames,
      };
    });

    if (inserts.length === 0) {
      return;
    }

    await this.db
      .insert(federatedGraphPersistedOperations)
      .values(inserts)
      .onConflictDoUpdate({
        target: [
          federatedGraphPersistedOperations.federatedGraphId,
          federatedGraphPersistedOperations.clientId,
          federatedGraphPersistedOperations.operationId,
        ],
        set: { updatedAt: now, updatedById: userId },
      });
  }

  public async getPersistedOperations(
    clientId: string,
    pagination?: {
      limit: number;
      offset: number;
    },
  ): Promise<PersistedOperationDTO[]> {
    const operationsResult = await this.db.query.federatedGraphPersistedOperations.findMany({
      where: and(
        eq(federatedGraphPersistedOperations.federatedGraphId, this.federatedGraphId),
        eq(federatedGraphPersistedOperations.clientId, clientId),
      ),
      with: {
        createdBy: true,
        updatedBy: true,
      },
      orderBy: desc(
        sql`coalesce(${federatedGraphPersistedOperations.updatedAt}, ${federatedGraphPersistedOperations.createdAt})`,
      ),
      offset: pagination?.offset,
      limit: pagination?.limit,
    });

    const operations: PersistedOperationDTO[] = [];

    for (const row of operationsResult) {
      operations.push({
        id: row.id,
        operationId: row.operationId,
        hash: row.hash,
        filePath: row.filePath,
        createdAt: row.createdAt.toISOString(),
        lastUpdatedAt: row?.updatedAt?.toISOString() || '',
        createdBy: row.createdBy?.email,
        lastUpdatedBy: row.updatedBy?.email ?? '',
        contents: row.operationContent ?? '',
        operationNames: row.operationNames ?? [],
      });
    }
    return operations;
  }

  public async getPersistedOperation({
    operationId,
  }: {
    operationId: string;
  }): Promise<PersistedOperationWithClientDTO | undefined> {
    const users1 = aliasedTable(users, 'users1');
    const users2 = aliasedTable(users, 'users2');

    const operationResult = await this.db
      .select({
        id: federatedGraphPersistedOperations.id,
        operationId: federatedGraphPersistedOperations.operationId,
        hash: federatedGraphPersistedOperations.hash,
        filePath: federatedGraphPersistedOperations.filePath,
        createdAt: federatedGraphPersistedOperations.createdAt,
        updatedAt: federatedGraphPersistedOperations.updatedAt,
        operationContent: federatedGraphPersistedOperations.operationContent,
        operationNames: federatedGraphPersistedOperations.operationNames,
        clientName: federatedGraphClients.name,
        createdBy: users1.email,
        updatedBy: users2.email,
      })
      .from(federatedGraphPersistedOperations)
      .innerJoin(federatedGraphClients, eq(federatedGraphClients.id, federatedGraphPersistedOperations.clientId))
      .leftJoin(users1, eq(users1.id, federatedGraphPersistedOperations.createdById))
      .leftJoin(users2, eq(users2.id, federatedGraphPersistedOperations.updatedById))
      .where(
        and(
          eq(federatedGraphPersistedOperations.federatedGraphId, this.federatedGraphId),
          eq(federatedGraphPersistedOperations.operationId, operationId),
        ),
      );

    if (operationResult.length === 0) {
      return undefined;
    }

    return {
      id: operationResult[0].id,
      operationId: operationResult[0].operationId,
      hash: operationResult[0].hash,
      filePath: operationResult[0].filePath,
      createdAt: operationResult[0].createdAt.toISOString(),
      lastUpdatedAt: operationResult[0]?.updatedAt?.toISOString() || '',
      createdBy: operationResult[0].createdBy ?? '',
      lastUpdatedBy: operationResult[0].updatedBy ?? '',
      contents: operationResult[0].operationContent ?? '',
      clientName: operationResult[0].clientName,
    };
  }

  public async registerClient(clientName: string, userId: string): Promise<string> {
    if (!clientName) {
      throw new Error('client name is empty');
    }
    const updatedAt = new Date();
    await this.db
      .insert(federatedGraphClients)
      .values({
        federatedGraphId: this.federatedGraphId,
        name: clientName,
        updatedAt,
        createdById: userId,
      })
      .onConflictDoUpdate({
        target: [federatedGraphClients.federatedGraphId, federatedGraphClients.name],
        set: { updatedAt, updatedById: userId },
      });

    // To avoid depending on postgres, we do a second query to get the inserted client
    const result = await this.db.query.federatedGraphClients.findFirst({
      columns: { id: true },
      where: and(
        eq(federatedGraphClients.name, clientName),
        eq(federatedGraphClients.federatedGraphId, this.federatedGraphId),
      ),
    });
    return result!.id;
  }

  public async getRegisteredClients(): Promise<ClientDTO[]> {
    const fedGraphClients = await this.db.query.federatedGraphClients.findMany({
      where: eq(federatedGraphClients.federatedGraphId, this.federatedGraphId),
      with: {
        createdBy: true,
        updatedBy: true,
      },
      orderBy: desc(sql`coalesce(${federatedGraphClients.updatedAt}, ${federatedGraphClients.createdAt})`),
    });
    const clients: ClientDTO[] = [];

    for (const c of fedGraphClients) {
      clients.push({
        id: c.id,
        name: c.name,
        createdAt: c.createdAt.toISOString(),
        lastUpdatedAt: c.updatedAt?.toISOString() || '',
        createdBy: c.createdBy?.email ?? '',
        lastUpdatedBy: c.updatedBy?.email ?? '',
      });
    }

    return clients;
  }

  public createOperationOverrides(data: {
    changes: PlainMessage<OverrideChange>[];
    namespaceId: string;
    operationHash: string;
    operationName: string;
    actorId: string;
  }) {
    return this.db
      .insert(schema.operationChangeOverrides)
      .values(
        data.changes.map((c) => ({
          namespaceId: data.namespaceId,
          hash: data.operationHash,
          name: data.operationName,
          changeType: c.changeType as SchemaChangeType,
          path: c.path,
          createdBy: data.actorId,
        })),
      )
      .onConflictDoUpdate({
        target: [
          schema.operationChangeOverrides.hash,
          schema.operationChangeOverrides.namespaceId,
          schema.operationChangeOverrides.changeType,
          schema.operationChangeOverrides.path,
        ],
        set: {
          namespaceId: data.namespaceId,
          hash: data.operationHash,
          name: data.operationName,
        },
      })
      .returning();
  }

  public removeOperationOverrides(data: {
    operationHash: string;
    namespaceId: string;
    changes: PlainMessage<OverrideChange>[];
  }) {
    return this.db.transaction(async (tx) => {
      const affectedRows = [];
      for (const change of data.changes) {
        const res = await tx
          .delete(schema.operationChangeOverrides)
          .where(
            and(
              eq(schema.operationChangeOverrides.hash, data.operationHash),
              eq(schema.operationChangeOverrides.namespaceId, data.namespaceId),
              eq(schema.operationChangeOverrides.changeType, change.changeType as SchemaChangeType),
              change.path
                ? eq(schema.operationChangeOverrides.path, change.path)
                : isNull(schema.operationChangeOverrides.path),
            ),
          )
          .returning();
        affectedRows.push(...res);
      }

      return affectedRows;
    });
  }

  public createIgnoreAllOverride(data: {
    namespaceId: string;
    operationHash: string;
    operationName: string;
    actorId: string;
  }) {
    return this.db
      .insert(schema.operationIgnoreAllOverrides)
      .values({
        namespaceId: data.namespaceId,
        hash: data.operationHash,
        name: data.operationName,
        createdBy: data.actorId,
      })
      .onConflictDoUpdate({
        target: [schema.operationIgnoreAllOverrides.hash, schema.operationIgnoreAllOverrides.namespaceId],
        set: {
          namespaceId: data.namespaceId,
          hash: data.operationHash,
          name: data.operationName,
        },
      })
      .returning();
  }

  public removeIgnoreAllOverride(data: { operationHash: string; namespaceId: string }) {
    return this.db
      .delete(schema.operationIgnoreAllOverrides)
      .where(
        and(
          eq(schema.operationIgnoreAllOverrides.namespaceId, data.namespaceId),
          eq(schema.operationIgnoreAllOverrides.hash, data.operationHash),
        ),
      )
      .returning();
  }

  public async hasIgnoreAllOverride(data: { operationHash: string; namespaceId: string }) {
    const res = await this.db.query.operationIgnoreAllOverrides.findFirst({
      columns: {
        id: true,
      },
      where: and(
        eq(schema.operationIgnoreAllOverrides.namespaceId, data.namespaceId),
        eq(schema.operationIgnoreAllOverrides.hash, data.operationHash),
      ),
    });

    return !!res;
  }

  public async getChangeOverridesByOperationHash(data: { operationHash: string; namespaceId: string }) {
    const res = await this.db.query.operationChangeOverrides.findMany({
      where: and(
        eq(schema.operationChangeOverrides.hash, data.operationHash),
        eq(schema.operationChangeOverrides.namespaceId, data.namespaceId),
      ),
      orderBy: desc(schema.operationChangeOverrides.createdAt),
    });

    return res.map((r) => ({
      ...r,
      createdAt: r.createdAt.toISOString(),
    }));
  }

  public async getChangeOverrides(data: { namespaceId: string }): Promise<ChangeOverride[]> {
    const res = await this.db.query.operationChangeOverrides.findMany({
      where: eq(schema.operationChangeOverrides.namespaceId, data.namespaceId),
      orderBy: desc(schema.operationChangeOverrides.createdAt),
    });

    return res.map((r) => ({
      ...r,
      createdAt: r.createdAt.toISOString(),
    }));
  }

  public async getIgnoreAllOverrides(data: { namespaceId: string }): Promise<IgnoreAllOverride[]> {
    const res = await this.db.query.operationIgnoreAllOverrides.findMany({
      where: eq(schema.operationIgnoreAllOverrides.namespaceId, data.namespaceId),
    });

    return res.map((r) => ({
      ...r,
      createdAt: r.createdAt.toISOString(),
    }));
  }

  // This function is used to determine if all operations have an ignore all override or if all their changes have an override
  // This is used to determine what options to show in the UI as the ui wont have the data of all the operations
  public async getOperationOverrideStatusOfCheck({
    checkId,
    checkDetails,
    overrides,
    ignoreAllOverrides,
  }: {
    checkId: string;
    checkDetails: SchemaCheckDetailsDTO;
    overrides: ChangeOverride[];
    ignoreAllOverrides: IgnoreAllOverride[];
  }) {
    const schemaCheckRepo = new SchemaCheckRepository(this.db);
    const affectedOperations = await schemaCheckRepo.getAffectedOperationsByCheckId({
      checkId,
    });

    const allOperations = affectedOperations.map((operation) => ({
      ...operation,
      impactingChanges: checkDetails.changes
        .filter(({ id }) => operation.schemaChangeIds.includes(id))
        .map((c) => ({
          ...c,
          hasOverride: overrides.some(
            (o) => o.hash === operation.hash && o.changeType === c.changeType && o.path === c.path,
          ),
        })),
      hasIgnoreAllOverride: ignoreAllOverrides.some((io) => io.hash === operation.hash),
    }));

    return {
      doAllOperationsHaveIgnoreAllOverride: allOperations.every((op) => op.hasIgnoreAllOverride),
      doAllOperationsHaveAllTheirChangesMarkedSafe: allOperations.every((op) =>
        op.impactingChanges.every((c) => !!c.hasOverride),
      ),
    };
  }

  public getConsolidatedOverridesView(data: { namespaceId: string }) {
    const change = this.db
      .select({
        hash: schema.operationChangeOverrides.hash,
        name: sql`max(${schema.operationChangeOverrides.name})`.as('change_operation_name'),
        namespaceId: schema.operationChangeOverrides.namespaceId,
        created_at: sql`max(${schema.operationChangeOverrides.createdAt})`.as('change_created_at'),
      })
      .from(schema.operationChangeOverrides)
      .where(eq(schema.operationChangeOverrides.namespaceId, data.namespaceId))
      .groupBy(({ hash, namespaceId }) => [hash, namespaceId])
      .as('change');

    const ignore = this.db
      .select({
        hash: schema.operationIgnoreAllOverrides.hash,
        name: sql`max(${schema.operationIgnoreAllOverrides.name})`.as('ignore_operation_name'),
        namespaceId: schema.operationIgnoreAllOverrides.namespaceId,
        created_at: sql`max(${schema.operationIgnoreAllOverrides.createdAt})`.as('ignore_created_at'),
      })
      .from(schema.operationIgnoreAllOverrides)
      .where(eq(schema.operationIgnoreAllOverrides.namespaceId, data.namespaceId))
      .groupBy(({ hash, namespaceId }) => [hash, namespaceId])
      .as('ignore');

    const changeCounts = this.db
      .select({
        hash: schema.operationChangeOverrides.hash,
        namespaceId: schema.operationChangeOverrides.namespaceId,
        change_count: sql`count(*)`.as('change_count'),
      })
      .from(schema.operationChangeOverrides)
      .where(eq(schema.operationChangeOverrides.namespaceId, data.namespaceId))
      .groupBy(({ hash, namespaceId }) => [hash, namespaceId])
      .as('change_counts');

    // One table stores ignore all override for an operation
    // The other table stores the specific changes that are overridden for an operation
    // We need to retrieve a consolidated view of overrides from both tables.
    // There is no guarantee that an entry for hash exists in both.

    return this.db
      .select({
        hash: sql<string>`coalesce(${change.hash}, ${ignore.hash})`,
        name: sql<string>`coalesce(${change.name}, ${ignore.name})`,
        updatedAt: sql`greatest(${change.created_at},${ignore.created_at})`.mapWith({
          mapFromDriverValue: (value) => new Date(value).toISOString(),
        }),
        hasIgnoreAllOverride: sql<boolean>`case when ${ignore.hash} is not null then true else false end`,
        changesOverrideCount: sql<number>`cast(coalesce(${changeCounts.change_count}, 0) as int)`,
      })
      .from(change)
      .fullJoin(ignore, and(eq(change.hash, ignore.hash), eq(change.namespaceId, ignore.namespaceId)))
      .leftJoin(changeCounts, and(eq(change.hash, changeCounts.hash), eq(change.namespaceId, changeCounts.namespaceId)))
      .orderBy(({ name, hash }) => [asc(name), asc(hash)]);
  }
}
