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
export type DataSource = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    type: string;
    paths: string[];
    lastScan: Timestamp | null;
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
    dataSourceId: string;
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
export type UserCollectionData = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    notes: string | null;
    rating: number | null;
    userId: string;
    collectionId: string;
};
export type UserCustomList = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    name: string;
    type: string;
    public: Generated<boolean>;
    userId: string;
};
export type UserCustomList_Collection = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    order: number;
    notes: string | null;
    userCustomListId: string;
    collectionId: string;
};
export type UserItemData = {
    id: string;
    createdAt: Generated<Timestamp>;
    updatedAt: Timestamp;
    /**
     * [UserItemDataProgress]
     */
    progress: unknown | null;
    completed: Generated<boolean>;
    bookmarked: Generated<boolean>;
    userId: string;
    itemId: string;
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
    DataSource: DataSource;
    DiskCollection: DiskCollection;
    DiskItem: DiskItem;
    Item: Item;
    User: User;
    UserCollectionData: UserCollectionData;
    UserCustomList: UserCustomList;
    UserCustomList_Collection: UserCustomList_Collection;
    UserItemData: UserItemData;
    UserSession: UserSession;
};
