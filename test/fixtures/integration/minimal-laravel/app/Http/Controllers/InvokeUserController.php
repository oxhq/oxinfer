<?php

namespace App\Http\Controllers;

use App\Models\User;
use Illuminate\Http\JsonResponse;

class InvokeUserController extends Controller
{
    public function __invoke(): JsonResponse
    {
        return response()->json(User::query()->count());
    }
}
