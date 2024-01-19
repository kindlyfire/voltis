import type { ColumnType } from "kysely";
export type Generated<T> = T extends ColumnType<infer S, infer I, infer U>
  ? ColumnType<S, I | undefined, U>
  : ColumnType<T, T | undefined, T>;
export type Timestamp = ColumnType<Date, Date | string, Date | string>;

export type Collection = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    nameOverride: string | null;
    type: string;
    contentUri: string;
    coverPath: string | null;
    /**
     * [CollectionMetadata]
     */
    metadata: unknown;
};
export type DiskCollection = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    type: string;
    path: string;
    coverPath: string | null;
    contentUri: string;
    contentUriOverride: string | null;
    missing: Generated<boolean>;
    libraryId: string;
};
export type DiskItem = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    path: string;
    coverPath: string | null;
    contentUri: string;
    /**
     * [DiskItemMetadata]
     */
    metadata: unknown;
    diskCollectionId: string;
};
export type Item = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    nameOverride: string | null;
    type: string;
    contentUri: string;
    coverPath: string | null;
    metadata: unknown;
    sortValue: number[];
    collectionId: string;
};
export type Library = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    type: string;
    paths: string[];
    lastScan: Timestamp | null;
};
export type User = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    username: string;
    email: string;
    password: string;
    roles: string[];
    preferences: unknown;
};
export type UserSession = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    token: string;
    userId: string;
};
export type DB = {
    Collection: Collection;
    DiskCollection: DiskCollection;
    DiskItem: DiskItem;
    Item: Item;
    Library: Library;
    User: User;
    UserSession: UserSession;
};
