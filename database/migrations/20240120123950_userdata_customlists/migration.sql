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
CREATE TABLE "UserCustomList_Item" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "order" INTEGER NOT NULL,
    "notes" TEXT,
    "userCustomListId" TEXT NOT NULL,
    "itemId" TEXT NOT NULL,

    CONSTRAINT "UserCustomList_Item_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "UserItemData_userId_itemId_key" ON "UserItemData"("userId", "itemId");

-- CreateIndex
CREATE UNIQUE INDEX "UserCollectionData_userId_collectionId_key" ON "UserCollectionData"("userId", "collectionId");

-- CreateIndex
CREATE UNIQUE INDEX "UserCustomList_Item_userCustomListId_itemId_key" ON "UserCustomList_Item"("userCustomListId", "itemId");

-- AddForeignKey
ALTER TABLE "UserItemData" ADD CONSTRAINT "UserItemData_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserItemData" ADD CONSTRAINT "UserItemData_itemId_fkey" FOREIGN KEY ("itemId") REFERENCES "Item"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCollectionData" ADD CONSTRAINT "UserCollectionData_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCollectionData" ADD CONSTRAINT "UserCollectionData_collectionId_fkey" FOREIGN KEY ("collectionId") REFERENCES "Collection"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList" ADD CONSTRAINT "UserCustomList_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList_Item" ADD CONSTRAINT "UserCustomList_Item_userCustomListId_fkey" FOREIGN KEY ("userCustomListId") REFERENCES "UserCustomList"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "UserCustomList_Item" ADD CONSTRAINT "UserCustomList_Item_itemId_fkey" FOREIGN KEY ("itemId") REFERENCES "Item"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
