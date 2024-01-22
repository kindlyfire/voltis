-- CreateTable
CREATE TABLE "User" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "username" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "password" TEXT NOT NULL,
    "roles" TEXT[],
    "preferences" JSONB NOT NULL,

    CONSTRAINT "User_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserSession" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "token" TEXT NOT NULL,
    "userId" TEXT NOT NULL,

    CONSTRAINT "UserSession_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "DataSource" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "paths" TEXT[],
    "lastScan" TIMESTAMP(3),

    CONSTRAINT "DataSource_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "DiskCollection" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "path" TEXT NOT NULL,
    "coverPath" TEXT,
    "contentUri" TEXT NOT NULL,
    "contentUriOverride" TEXT,
    "missing" BOOLEAN NOT NULL DEFAULT false,
    "dataSourceId" TEXT NOT NULL,

    CONSTRAINT "DiskCollection_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "DiskItem" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "path" TEXT NOT NULL,
    "coverPath" TEXT,
    "contentUri" TEXT NOT NULL,
    "metadata" JSONB NOT NULL,
    "diskCollectionId" TEXT NOT NULL,

    CONSTRAINT "DiskItem_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Collection" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "nameOverride" TEXT,
    "type" TEXT NOT NULL,
    "contentUri" TEXT NOT NULL,
    "coverPath" TEXT,
    "metadata" JSONB NOT NULL,

    CONSTRAINT "Collection_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Item" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "nameOverride" TEXT,
    "type" TEXT NOT NULL,
    "contentUri" TEXT NOT NULL,
    "coverPath" TEXT,
    "metadata" JSONB NOT NULL,
    "sortValue" INTEGER[],
    "collectionId" TEXT NOT NULL,

    CONSTRAINT "Item_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserItemData" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "progress" JSONB,
    "completed" BOOLEAN NOT NULL DEFAULT false,
    "bookmarked" BOOLEAN NOT NULL DEFAULT false,
    "userId" TEXT NOT NULL,
    "itemId" TEXT NOT NULL,

    CONSTRAINT "UserItemData_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserCollectionData" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "notes" TEXT,
    "rating" INTEGER,
    "userId" TEXT NOT NULL,
    "collectionId" TEXT NOT NULL,

    CONSTRAINT "UserCollectionData_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserCustomList" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "name" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "public" BOOLEAN NOT NULL DEFAULT false,
    "userId" TEXT NOT NULL,

    CONSTRAINT "UserCustomList_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "UserCustomList_Collection" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "order" INTEGER NOT NULL,
    "notes" TEXT,
    "userCustomListId" TEXT NOT NULL,
    "collectionId" TEXT NOT NULL,

    CONSTRAINT "UserCustomList_Collection_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");

-- CreateIndex
CREATE UNIQUE INDEX "UserSession_token_key" ON "UserSession"("token");

-- CreateIndex
CREATE UNIQUE INDEX "Collection_contentUri_key" ON "Collection"("contentUri");

-- CreateIndex
CREATE UNIQUE INDEX "UserItemData_userId_itemId_key" ON "UserItemData"("userId", "itemId");

-- CreateIndex
CREATE UNIQUE INDEX "UserCollectionData_userId_collectionId_key" ON "UserCollectionData"("userId", "collectionId");

-- AddForeignKey
ALTER TABLE "UserSession" ADD CONSTRAINT "UserSession_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "DiskCollection" ADD CONSTRAINT "DiskCollection_dataSourceId_fkey" FOREIGN KEY ("dataSourceId") REFERENCES "DataSource"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "DiskItem" ADD CONSTRAINT "DiskItem_diskCollectionId_fkey" FOREIGN KEY ("diskCollectionId") REFERENCES "DiskCollection"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Item" ADD CONSTRAINT "Item_collectionId_fkey" FOREIGN KEY ("collectionId") REFERENCES "Collection"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserItemData" ADD CONSTRAINT "UserItemData_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserItemData" ADD CONSTRAINT "UserItemData_itemId_fkey" FOREIGN KEY ("itemId") REFERENCES "Item"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCollectionData" ADD CONSTRAINT "UserCollectionData_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCollectionData" ADD CONSTRAINT "UserCollectionData_collectionId_fkey" FOREIGN KEY ("collectionId") REFERENCES "Collection"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList" ADD CONSTRAINT "UserCustomList_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList_Collection" ADD CONSTRAINT "UserCustomList_Collection_userCustomListId_fkey" FOREIGN KEY ("userCustomListId") REFERENCES "UserCustomList"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList_Collection" ADD CONSTRAINT "UserCustomList_Collection_collectionId_fkey" FOREIGN KEY ("collectionId") REFERENCES "Collection"("id") ON DELETE CASCADE ON UPDATE CASCADE;
