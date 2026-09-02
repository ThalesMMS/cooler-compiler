class A inherits B {};
class B inherits A {};
class C inherits D {};
class D inherits E {};
class E inherits C {};
class RecoveredChild inherits A {};
class Main {
  main() : Object { 0 };
};
